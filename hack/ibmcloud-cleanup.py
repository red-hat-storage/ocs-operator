#!/usr/bin/env python3
"""Delete aged OpenShift CI leftovers in IBM Cloud (odf-openshift-ci).

Targets ci-op-* VPC stacks older than AGE_HOURS, tearing them down in
dependency order, then aged ci-op-* COS / image-registry service instances.

Keeps permanent resources (CIS, SSH keys, anything not named ci-op-*).

Usage:
  export IBMCLOUD_API_KEY=...
  python3 hack/ibmcloud-cleanup.py --dry-run
  python3 hack/ibmcloud-cleanup.py
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone
from typing import Any

DEFAULT_AGE_HOURS = 6
DEFAULT_RESOURCE_GROUP = "odf-openshift-ci"
DEFAULT_REGIONS = ("us-east", "us-south")
KEEP_SERVICE_INSTANCES = frozenset({"odf-openshift-ci-cis"})
GONE = ("not found", "does not exist", "could not find", "no floating ip")


def log(msg: str) -> None:
    print(msg, flush=True)


def run(args: list[str], check: bool = False) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, capture_output=True, text=True, check=check)


def ibmcloud(*args: str) -> subprocess.CompletedProcess[str]:
    return run(["ibmcloud", *args])


def ibmcloud_json(*args: str) -> Any:
    p = ibmcloud(*args, "--output", "json")
    if p.returncode != 0:
        err = (p.stderr or p.stdout or "").strip()
        raise RuntimeError(f"ibmcloud {' '.join(args)} failed: {err[:300]}")
    if not p.stdout.strip():
        return []
    return json.loads(p.stdout)


def is_ci(name: str | None) -> bool:
    return bool(name) and name.startswith("ci-op-")


def parse_created(value: str | None) -> datetime | None:
    if not value:
        return None
    # IBM returns e.g. 2026-11-12T05:48:58.000Z
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def is_aged(obj: dict[str, Any], cutoff: datetime) -> bool:
    created = parse_created(obj.get("created_at") or obj.get("created_on"))
    if created is None:
        return False
    return created <= cutoff


def infra_from_vpc(vpc_name: str) -> str:
    return vpc_name[:-4] if vpc_name.endswith("-vpc") else vpc_name


def belongs(name: str | None, infra: str, vpc_id: str | None, obj_vpc_id: str | None) -> bool:
    if name and (name.startswith(infra) or infra in name):
        return True
    if vpc_id and obj_vpc_id == vpc_id:
        return True
    return False


def delete(args: list[str], label: str, dry_run: bool, attempts: int = 6, sleep_s: int = 8) -> bool:
    if dry_run:
        log(f"  DRY-RUN delete {label}")
        return True
    for i in range(1, attempts + 1):
        p = run(args)
        if p.returncode == 0:
            log(f"  deleted {label}")
            return True
        err = (p.stderr or p.stdout or "").strip().replace("\n", " ")
        low = err.lower()
        if any(x in low for x in GONE):
            log(f"  gone {label}")
            return True
        log(f"  retry {i}/{attempts} {label}: {err[:160]}")
        time.sleep(sleep_s)
    log(f"  FAILED {label}")
    return False


def login(api_key: str, resource_group: str) -> None:
    # Avoid interactive version checks stalling CI.
    ibmcloud("config", "--check-version=false")
    p = ibmcloud("login", "--apikey", api_key, "-g", resource_group, "-q")
    if p.returncode != 0:
        raise RuntimeError(f"ibmcloud login failed: {(p.stderr or p.stdout or '')[:300]}")
    log(f"logged in; resource group={resource_group}")


def target(region: str, resource_group: str) -> None:
    p = ibmcloud("target", "-r", region, "-g", resource_group)
    if p.returncode != 0:
        raise RuntimeError(f"ibmcloud target {region} failed: {(p.stderr or p.stdout or '')[:300]}")


def clear_sg_rules(sg_id: str, sg_name: str, dry_run: bool) -> None:
    try:
        rules = ibmcloud_json("is", "security-group-rules", sg_id)
    except RuntimeError as e:
        log(f"  WARN list rules {sg_name}: {e}")
        return
    if not isinstance(rules, list):
        rules = []
    for rule in rules:
        rid = rule.get("id")
        if not rid:
            continue
        delete(
            ["ibmcloud", "is", "security-group-rule-delete", sg_id, rid, "-f"],
            f"sg-rule/{sg_name}/{rid}",
            dry_run,
            attempts=3,
            sleep_s=2,
        )


def cleanup_vpc(vpc: dict[str, Any], dry_run: bool) -> bool:
    vpc_id = vpc["id"]
    vpc_name = vpc["name"]
    infra = infra_from_vpc(vpc_name)
    log(f"VPC {vpc_name} (infra={infra})")

    ok = True

    # 1) Load balancers
    for lb in ibmcloud_json("is", "load-balancers"):
        if belongs(lb.get("name"), infra, vpc_id, (lb.get("vpc") or {}).get("id")):
            ok &= delete(
                ["ibmcloud", "is", "load-balancer-delete", lb["id"], "-f"],
                f"lb/{lb.get('name')}",
                dry_run,
            )

    # 2) Instances
    for inst in ibmcloud_json("is", "instances"):
        if belongs(inst.get("name"), infra, vpc_id, (inst.get("vpc") or {}).get("id")):
            ok &= delete(
                ["ibmcloud", "is", "instance-delete", inst["id"], "-f"],
                f"instance/{inst.get('name')}",
                dry_run,
                attempts=8,
                sleep_s=10,
            )

    # 3) Endpoint gateways
    for egw in ibmcloud_json("is", "endpoint-gateways"):
        if belongs(egw.get("name"), infra, vpc_id, (egw.get("vpc") or {}).get("id")):
            ok &= delete(
                ["ibmcloud", "is", "endpoint-gateway-delete", egw["id"], "-f"],
                f"egw/{egw.get('name')}",
                dry_run,
            )

    # 4) Detach public gateways from subnets, then delete subnets
    for sn in ibmcloud_json("is", "subnets"):
        if not belongs(sn.get("name"), infra, vpc_id, (sn.get("vpc") or {}).get("id")):
            continue
        if sn.get("public_gateway"):
            delete(
                ["ibmcloud", "is", "subnet-public-gateway-detach", sn["id"], "-f"],
                f"detach-pgw/{sn.get('name')}",
                dry_run,
                attempts=4,
                sleep_s=3,
            )
        ok &= delete(
            ["ibmcloud", "is", "subnet-delete", sn["id"], "-f"],
            f"subnet/{sn.get('name')}",
            dry_run,
            attempts=10,
            sleep_s=8,
        )

    # 5) Public gateways
    for pgw in ibmcloud_json("is", "public-gateways"):
        if belongs(pgw.get("name"), infra, vpc_id, (pgw.get("vpc") or {}).get("id")):
            ok &= delete(
                ["ibmcloud", "is", "public-gateway-delete", pgw["id"], "-f"],
                f"pgw/{pgw.get('name')}",
                dry_run,
                attempts=8,
                sleep_s=6,
            )

    # 6) Floating IPs
    for fip in ibmcloud_json("is", "floating-ips"):
        if belongs(fip.get("name"), infra, vpc_id, None):
            ok &= delete(
                ["ibmcloud", "is", "floating-ip-release", fip["id"], "-f"],
                f"fip/{fip.get('name')}",
                dry_run,
            )

    # 7) Volumes
    for vol in ibmcloud_json("is", "volumes"):
        if belongs(vol.get("name"), infra, vpc_id, None):
            ok &= delete(
                ["ibmcloud", "is", "volume-delete", vol["id"], "-f"],
                f"volume/{vol.get('name')}",
                dry_run,
                attempts=8,
                sleep_s=8,
            )

    # 8) Security groups: clear rules, then delete non-default
    sgs = [
        sg
        for sg in ibmcloud_json("is", "security-groups")
        if not sg.get("is_default")
        and belongs(sg.get("name"), infra, vpc_id, (sg.get("vpc") or {}).get("id"))
    ]
    for sg in sgs:
        clear_sg_rules(sg["id"], sg.get("name") or sg["id"], dry_run)
    for sg in sgs:
        ok &= delete(
            ["ibmcloud", "is", "security-group-delete", sg["id"], "-f"],
            f"sg/{sg.get('name')}",
            dry_run,
            attempts=5,
            sleep_s=4,
        )

    # 9) Non-default network ACLs
    for nacl in ibmcloud_json("is", "network-acls"):
        if nacl.get("is_default"):
            continue
        if belongs(nacl.get("name"), infra, vpc_id, (nacl.get("vpc") or {}).get("id")):
            ok &= delete(
                ["ibmcloud", "is", "network-acl-delete", nacl["id"], "-f"],
                f"nacl/{nacl.get('name')}",
                dry_run,
            )

    # 10) VPC
    ok &= delete(
        ["ibmcloud", "is", "vpc-delete", vpc_id, "-f"],
        f"vpc/{vpc_name}",
        dry_run,
        attempts=10,
        sleep_s=10,
    )
    return ok


def cleanup_region(region: str, resource_group: str, cutoff: datetime, dry_run: bool) -> bool:
    log(f"\n===== {region} =====")
    target(region, resource_group)
    vpcs = [
        v
        for v in ibmcloud_json("is", "vpcs")
        if is_ci(v.get("name")) and is_aged(v, cutoff)
    ]
    log(f"aged ci-op VPCs: {len(vpcs)}")
    for v in vpcs:
        log(f"  - {v['name']} created={v.get('created_at')}")

    ok = True
    for vpc in vpcs:
        ok &= cleanup_vpc(vpc, dry_run)
    return ok


def cleanup_service_instances(resource_group: str, cutoff: datetime, dry_run: bool) -> bool:
    log("\n===== service instances =====")
    items = ibmcloud_json("resource", "service-instances", "-g", resource_group)
    if not isinstance(items, list):
        items = []

    ok = True
    for it in items:
        name = it.get("name") or ""
        if name in KEEP_SERVICE_INSTANCES:
            log(f"KEEP {name}")
            continue
        if not is_ci(name):
            log(f"SKIP {name}")
            continue
        # resource controller uses created_at
        if not is_aged(it, cutoff):
            log(f"YOUNG {name} created={it.get('created_at')}")
            continue
        ok &= delete(
            [
                "ibmcloud",
                "resource",
                "service-instance-delete",
                name,
                "-g",
                resource_group,
                "-f",
                "--recursive",
            ],
            f"si/{name}",
            dry_run,
            attempts=6,
            sleep_s=8,
        )
    return ok


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--dry-run", action="store_true", help="Print actions without deleting")
    p.add_argument(
        "--age-hours",
        type=float,
        default=float(os.environ.get("AGE_HOURS", DEFAULT_AGE_HOURS)),
        help=f"Only delete resources older than this many hours (default {DEFAULT_AGE_HOURS})",
    )
    p.add_argument(
        "--resource-group",
        default=os.environ.get("RESOURCE_GROUP", DEFAULT_RESOURCE_GROUP),
    )
    p.add_argument(
        "--regions",
        default=os.environ.get("REGIONS", ",".join(DEFAULT_REGIONS)),
        help="Comma-separated IBM Cloud regions",
    )
    return p.parse_args()


def main() -> int:
    args = parse_args()
    api_key = os.environ.get("IBMCLOUD_API_KEY", "").strip()
    if not api_key:
        log("ERROR: IBMCLOUD_API_KEY is required")
        return 1

    regions = [r.strip() for r in args.regions.split(",") if r.strip()]
    cutoff = datetime.now(timezone.utc) - timedelta(hours=args.age_hours)
    log(
        f"ibmcloud-cleanup dry_run={args.dry_run} age_hours={args.age_hours} "
        f"cutoff={cutoff.isoformat()} rg={args.resource_group} regions={regions}"
    )

    login(api_key, args.resource_group)

    ok = True
    for region in regions:
        ok &= cleanup_region(region, args.resource_group, cutoff, args.dry_run)
    ok &= cleanup_service_instances(args.resource_group, cutoff, args.dry_run)

    log("\nDONE" + ("" if ok else " with failures"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
