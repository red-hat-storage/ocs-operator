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
# Default VPC SGs/NACLs return this; they are removed with the VPC.
SKIPPABLE = ("not_authorized",)
# FIPs attached to a public gateway cannot be released until the PGW is gone.
FIP_IN_USE = ("in use", "conflict_field")
VPC_IN_USE = ("vpc_in_use", "in use and cannot be deleted")
SUBNET_IN_USE = ("subnet_in_use", "network_interface_exists")


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
    text = p.stdout.strip()
    if not text:
        return []
    try:
        return json.loads(text)
    except json.JSONDecodeError as e:
        raise RuntimeError(f"ibmcloud {' '.join(args)}: invalid JSON: {e}") from e


def is_list(data: Any) -> list[Any]:
    return data if isinstance(data, list) else []


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


def delete(
    args: list[str],
    label: str,
    dry_run: bool,
    attempts: int = 6,
    sleep_s: int = 8,
    soft_fail: tuple[str, ...] = (),
) -> bool:
    if dry_run:
        log(f"  DRY-RUN delete {label}")
        return True
    last_err = ""
    for i in range(1, attempts + 1):
        p = run(args)
        if p.returncode == 0:
            log(f"  deleted {label}")
            return True
        err = (p.stderr or p.stdout or "").strip().replace("\n", " ")
        last_err = err
        low = err.lower()
        if any(x in low for x in GONE):
            log(f"  gone {label}")
            return True
        if any(x in low for x in SKIPPABLE):
            log(f"  skip {label}: {err[:120]}")
            return True
        log(f"  retry {i}/{attempts} {label}: {err[:160]}")
        time.sleep(sleep_s)
    if soft_fail and any(x in last_err.lower() for x in soft_fail):
        log(f"  skip {label}: still in use after retries (will retry next run)")
        return True
    log(f"  FAILED {label}")
    return False


def wait_gone(
    list_args: list[str],
    ids: set[str],
    label: str,
    dry_run: bool,
    timeout_s: int = 300,
    poll_s: int = 10,
) -> set[str]:
    """Block until the given resource IDs no longer appear in a list call.

    Returns any IDs still present after the timeout (empty on success).
    """
    if dry_run or not ids:
        return set()
    deadline = time.time() + timeout_s
    pending = set(ids)
    while pending and time.time() < deadline:
        try:
            items = is_list(ibmcloud_json(*list_args))
        except RuntimeError as e:
            log(f"  WARN wait {label}: {e}")
            return pending
        present = {i.get("id") for i in items if i.get("id")}
        pending &= present
        if not pending:
            log(f"  waited: {label} gone")
            return set()
        time.sleep(poll_s)
    if pending:
        log(f"  WARN wait {label}: still present after {timeout_s}s ({len(pending)} left)")
    return pending


def obj_vpc_id(obj: dict[str, Any]) -> str | None:
    vpc = obj.get("vpc")
    if isinstance(vpc, dict):
        return vpc.get("id")
    if isinstance(vpc, str):
        return vpc
    return None


def fip_target_id(fip: dict[str, Any]) -> str | None:
    target = fip.get("target") or {}
    if not isinstance(target, dict):
        return None
    return target.get("id")


def fip_target_type(fip: dict[str, Any]) -> str | None:
    target = fip.get("target") or {}
    if not isinstance(target, dict):
        return None
    return target.get("resource_type") or target.get("name")


def login(api_key: str, resource_group: str, region: str) -> None:
    # Avoid interactive version checks / region prompts stalling CI.
    ibmcloud("config", "--check-version=false")
    p = ibmcloud(
        "login",
        "--apikey",
        api_key,
        "-r",
        region,
        "-g",
        resource_group,
        "-q",
    )
    if p.returncode != 0:
        raise RuntimeError(f"ibmcloud login failed: {(p.stderr or p.stdout or '')[:300]}")
    log(f"logged in; region={region} resource group={resource_group}")


def target(region: str, resource_group: str) -> None:
    p = ibmcloud("target", "-r", region, "-g", resource_group)
    if p.returncode != 0:
        raise RuntimeError(f"ibmcloud target {region} failed: {(p.stderr or p.stdout or '')[:300]}")


def clear_sg_rules(sg_id: str, sg_name: str, dry_run: bool) -> None:
    try:
        rules = is_list(ibmcloud_json("is", "security-group-rules", sg_id))
    except RuntimeError as e:
        log(f"  WARN list rules {sg_name}: {e}")
        return
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


def delete_matching_lbs(infra: str, vpc_id: str, dry_run: bool) -> set[str]:
    ids: set[str] = set()
    for lb in is_list(ibmcloud_json("is", "load-balancers")):
        if not belongs(lb.get("name"), infra, vpc_id, obj_vpc_id(lb)):
            continue
        ids.add(lb["id"])
        delete(
            ["ibmcloud", "is", "load-balancer-delete", lb["id"], "-f"],
            f"lb/{lb.get('name')}",
            dry_run,
            attempts=8,
            sleep_s=10,
        )
    return ids


def delete_matching_instances(infra: str, vpc_id: str, dry_run: bool) -> set[str]:
    ids: set[str] = set()
    for inst in is_list(ibmcloud_json("is", "instances")):
        if not belongs(inst.get("name"), infra, vpc_id, obj_vpc_id(inst)):
            continue
        ids.add(inst["id"])
        delete(
            ["ibmcloud", "is", "instance-delete", inst["id"], "-f"],
            f"instance/{inst.get('name')}",
            dry_run,
            attempts=8,
            sleep_s=10,
        )
    return ids


def collect_pgw_ids(infra: str, vpc_id: str) -> dict[str, str]:
    """Map public-gateway id -> label for this VPC / infra."""
    found: dict[str, str] = {}
    for pgw in is_list(ibmcloud_json("is", "public-gateways")):
        if not belongs(pgw.get("name"), infra, vpc_id, obj_vpc_id(pgw)):
            continue
        pid = pgw.get("id")
        if pid:
            found[pid] = pgw.get("name") or pid
    # Also discover PGWs only referenced by still-bound FIPs (list misses).
    for fip in is_list(ibmcloud_json("is", "floating-ips")):
        if not belongs(fip.get("name"), infra, None, None):
            continue
        if fip_target_type(fip) != "public_gateway":
            continue
        pid = fip_target_id(fip)
        if pid and pid not in found:
            found[pid] = f"from-fip/{fip.get('name')}"
            log(f"  note: pgw {pid} only visible via fip/{fip.get('name')}")
    return found


def delete_pgws(infra: str, vpc_id: str, dry_run: bool) -> bool:
    ok = True
    pgws = collect_pgw_ids(infra, vpc_id)
    if not pgws:
        return True
    for pid, label in pgws.items():
        ok &= delete(
            ["ibmcloud", "is", "public-gateway-delete", pid, "-f"],
            f"pgw/{label}",
            dry_run,
            attempts=8,
            sleep_s=6,
        )
    remaining = wait_gone(["is", "public-gateways"], set(pgws), "public-gateways", dry_run)
    # Force another pass for anything still listed or still referenced by FIPs.
    if remaining or any(
        fip_target_type(f) == "public_gateway"
        for f in is_list(ibmcloud_json("is", "floating-ips"))
        if belongs(f.get("name"), infra, None, None)
    ):
        for pid, label in collect_pgw_ids(infra, vpc_id).items():
            ok &= delete(
                ["ibmcloud", "is", "public-gateway-delete", pid, "-f"],
                f"pgw/{label}",
                dry_run,
                attempts=6,
                sleep_s=8,
            )
        wait_gone(["is", "public-gateways"], set(collect_pgw_ids(infra, vpc_id)), "public-gateways", dry_run)
    return ok


def release_fips(infra: str, dry_run: bool) -> bool:
    ok = True
    bound_pgw = 0
    for fip in is_list(ibmcloud_json("is", "floating-ips")):
        if not belongs(fip.get("name"), infra, None, None):
            continue
        kind = fip_target_type(fip)
        if kind == "public_gateway":
            bound_pgw += 1
            pid = fip_target_id(fip)
            if pid:
                delete(
                    ["ibmcloud", "is", "public-gateway-delete", pid, "-f"],
                    f"pgw/from-fip/{fip.get('name')}",
                    dry_run,
                    attempts=6,
                    sleep_s=8,
                )
            # Fall through and try release after PGW delete attempt.
        elif kind:
            log(f"  skip fip/{fip.get('name')}: still bound to {kind}")
            continue
        ok &= delete(
            ["ibmcloud", "is", "floating-ip-release", fip["id"], "-f"],
            f"fip/{fip.get('name')}",
            dry_run,
            attempts=10,
            sleep_s=8,
            soft_fail=FIP_IN_USE,
        )
    if bound_pgw:
        log(f"  note: {bound_pgw} fip(s) were still bound to public_gateway")
    return ok


def delete_subnets(infra: str, vpc_id: str, dry_run: bool) -> bool:
    ok = True
    for sn in is_list(ibmcloud_json("is", "subnets")):
        if not belongs(sn.get("name"), infra, vpc_id, obj_vpc_id(sn)):
            continue
        if sn.get("public_gateway"):
            delete(
                ["ibmcloud", "is", "subnet-public-gateway-detach", sn["id"], "-f"],
                f"detach-pgw/{sn.get('name')}",
                dry_run,
                attempts=4,
                sleep_s=3,
            )
        # First attempt; on NIC-in-use, re-delete leftover instances/LBs then retry.
        if delete(
            ["ibmcloud", "is", "subnet-delete", sn["id"], "-f"],
            f"subnet/{sn.get('name')}",
            dry_run,
            attempts=4,
            sleep_s=8,
        ):
            continue
        log(f"  note: re-sweeping lbs/instances before retrying subnet/{sn.get('name')}")
        delete_matching_lbs(infra, vpc_id, dry_run)
        leftover_inst = delete_matching_instances(infra, vpc_id, dry_run)
        wait_gone(["is", "instances"], leftover_inst, "instances", dry_run, timeout_s=120)
        ok &= delete(
            ["ibmcloud", "is", "subnet-delete", sn["id"], "-f"],
            f"subnet/{sn.get('name')}",
            dry_run,
            attempts=12,
            sleep_s=10,
            soft_fail=SUBNET_IN_USE,
        )
    return ok


def delete_vpc(vpc_id: str, vpc_name: str, infra: str, dry_run: bool) -> bool:
    """Delete VPC, re-sweeping public gateways if IBM reports vpc_in_use."""
    if dry_run:
        log(f"  DRY-RUN delete vpc/{vpc_name}")
        return True
    for i in range(1, 11):
        p = run(["ibmcloud", "is", "vpc-delete", vpc_id, "-f"])
        if p.returncode == 0:
            log(f"  deleted vpc/{vpc_name}")
            return True
        err = (p.stderr or p.stdout or "").strip().replace("\n", " ")
        low = err.lower()
        if any(x in low for x in GONE):
            log(f"  gone vpc/{vpc_name}")
            return True
        if any(x in low for x in VPC_IN_USE):
            log(f"  vpc_in_use vpc/{vpc_name}: {err[:200]}")
            delete_pgws(infra, vpc_id, dry_run)
            delete_subnets(infra, vpc_id, dry_run)
            release_fips(infra, dry_run)
            time.sleep(8)
            continue
        log(f"  retry {i}/10 vpc/{vpc_name}: {err[:160]}")
        time.sleep(10)
    log(f"  FAILED vpc/{vpc_name}")
    return False


def cleanup_vpc(vpc: dict[str, Any], dry_run: bool) -> bool:
    vpc_id = vpc["id"]
    vpc_name = vpc["name"]
    infra = infra_from_vpc(vpc_name)
    log(f"VPC {vpc_name} (infra={infra})")

    ok = True

    # 1) Load balancers — must finish before subnets can go.
    lb_ids = delete_matching_lbs(infra, vpc_id, dry_run)
    leftover = wait_gone(["is", "load-balancers"], lb_ids, "load-balancers", dry_run)
    if leftover:
        log("  note: re-deleting leftover load-balancers")
        lb_ids |= delete_matching_lbs(infra, vpc_id, dry_run)
        wait_gone(["is", "load-balancers"], lb_ids, "load-balancers", dry_run, timeout_s=180)

    # 2) Instances
    inst_ids = delete_matching_instances(infra, vpc_id, dry_run)
    leftover = wait_gone(["is", "instances"], inst_ids, "instances", dry_run)
    if leftover:
        log("  note: re-deleting leftover instances")
        inst_ids |= delete_matching_instances(infra, vpc_id, dry_run)
        wait_gone(["is", "instances"], inst_ids, "instances", dry_run, timeout_s=180)

    # 3) Endpoint gateways
    for egw in is_list(ibmcloud_json("is", "endpoint-gateways")):
        if belongs(egw.get("name"), infra, vpc_id, obj_vpc_id(egw)):
            ok &= delete(
                ["ibmcloud", "is", "endpoint-gateway-delete", egw["id"], "-f"],
                f"egw/{egw.get('name')}",
                dry_run,
            )

    # 4) Detach public gateways from subnets, then delete subnets
    ok &= delete_subnets(infra, vpc_id, dry_run)

    # 5) Public gateways — include orphans only reachable via FIP targets.
    ok &= delete_pgws(infra, vpc_id, dry_run)

    # 6) Floating IPs (delete PGW by FIP target id if still bound).
    ok &= release_fips(infra, dry_run)

    # 7) Volumes
    for vol in is_list(ibmcloud_json("is", "volumes")):
        if belongs(vol.get("name"), infra, None, None):
            ok &= delete(
                ["ibmcloud", "is", "volume-delete", vol["id"], "-f"],
                f"volume/{vol.get('name')}",
                dry_run,
                attempts=8,
                sleep_s=8,
            )

    # 8) Security groups: only installer-named ones (ci-op-*).
    # IBM auto-named default SGs (random words) return not_authorized and
    # are removed automatically with the VPC.
    sgs = [
        sg
        for sg in is_list(ibmcloud_json("is", "security-groups"))
        if not sg.get("is_default")
        and is_ci(sg.get("name"))
        and belongs(sg.get("name"), infra, vpc_id, obj_vpc_id(sg))
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

    # 9) Network ACLs: same naming filter as SGs (skip IBM defaults).
    for nacl in is_list(ibmcloud_json("is", "network-acls")):
        if nacl.get("is_default") or not is_ci(nacl.get("name")):
            continue
        if belongs(nacl.get("name"), infra, vpc_id, obj_vpc_id(nacl)):
            ok &= delete(
                ["ibmcloud", "is", "network-acl-delete", nacl["id"], "-f"],
                f"nacl/{nacl.get('name')}",
                dry_run,
            )

    # 10) VPC — re-sweep PGWs/subnets on vpc_in_use.
    ok &= delete_vpc(vpc_id, vpc_name, infra, dry_run)
    return ok


def cleanup_region(region: str, resource_group: str, cutoff: datetime, dry_run: bool) -> bool:
    log(f"\n===== {region} =====")
    target(region, resource_group)
    vpcs = [
        v
        for v in is_list(ibmcloud_json("is", "vpcs"))
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
    items = is_list(ibmcloud_json("resource", "service-instances", "-g", resource_group))

    ok = True
    for it in items:
        name = it.get("name") or ""
        if name in KEEP_SERVICE_INSTANCES:
            log(f"KEEP {name}")
            continue
        if not is_ci(name):
            log(f"SKIP {name}")
            continue
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
    if not regions:
        log("ERROR: at least one region is required")
        return 1

    cutoff = datetime.now(timezone.utc) - timedelta(hours=args.age_hours)
    log(
        f"ibmcloud-cleanup dry_run={args.dry_run} age_hours={args.age_hours} "
        f"cutoff={cutoff.isoformat()} rg={args.resource_group} regions={regions}"
    )

    login(api_key, args.resource_group, regions[0])

    ok = True
    for region in regions:
        ok &= cleanup_region(region, args.resource_group, cutoff, args.dry_run)
    ok &= cleanup_service_instances(args.resource_group, cutoff, args.dry_run)

    log("\nDONE" + ("" if ok else " with failures"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
