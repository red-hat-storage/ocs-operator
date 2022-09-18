#!/usr/bin/env bash
#
# Verify the CI index db of the current commit
# is up to date.
#

source hack/common.sh

set -e

INDEXDB="./openshift-ci/database/index.db"
BUILDINDEXDB="./openshift-ci/build-operator-index-ci.sh"

#
# Check the original CI index db exists
#
if [ ! -f ${INDEXDB} ]; then
	echo "Latest ${INDEXDB} has not been generated"
        echo "Run"
        echo "    ${BUILDINDEXDB}"
        echo "to generate CI index database. Then commit results."
	exit 1
fi

#
# Verify the CI index db is commited
#
if [[ -n "$(git status --porcelain ${INDEXDB} )" ]]; then
	echo "Uncommitted CI index db changes. Commit your results."
	exit 1
fi

#
# Dump the original CI index db
#
ORIGSQL=$(mktemp)
if ! sqlite3 ${INDEXDB} .dump | grep -v schema_migrations | uniq | sort > ${ORIGSQL}; then
	echo "Failed to dump original CI index db"
	exit 1
fi

#
# Update the CI index db
#
if ! ${BUILDINDEXDB};  then
	echo "Failed to run ${BUILDINDEXDB}"
	exit 1
fi

#
# Dump the generated CI index db
#
NEWSQL=$(mktemp)
if ! sqlite3 ${INDEXDB} .dump | grep -v schema_migrations | uniq | sort > ${NEWSQL}; then
	echo "Failed to dump new CI index db"
	exit 1
fi

#
# Poor's man compare of the database content
#
if ! diff -u ${ORIGSQL} ${NEWSQL}; then
	echo "uncommitted CI index database. run ${BUILDINDEXDB} and commit results."
	exit 1
fi

#
# Clean up
#
rm -rf ${ORIGSQL} ${NEWSQL}
git checkout ${INDEXDB}

echo "Success: ${INDEXDB} is up to date"
