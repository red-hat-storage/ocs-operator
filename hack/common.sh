#!/bin/bash

IMAGE_RUN_CMD="${IMAGE_RUN_CMD:-docker run --rm -it}"

OUTDIR="build/_output"
OUTDIR_TEMPLATES="$OUTDIR/csv-templates"
OUTDIR_CRDS="$OUTDIR_TEMPLATES/crds"
OUTDIR_TOOLS="$OUTDIR/tools"
NOOBAA_CSV=$OUTDIR_TEMPLATES/noobaa-csv.yaml
ROOK_CSV=$OUTDIR_TEMPLATES/rook-csv.yaml.in
OCS_CSV="$OUTDIR_TEMPLATES/ocs-operator.csv.yaml.in"
