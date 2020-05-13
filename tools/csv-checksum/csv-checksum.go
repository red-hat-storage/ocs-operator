package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"log"
	"path/filepath"
	"sort"
	"strings"
)

var (
	csvVersion         = flag.String("csv-version", "", "the unified CSV version")
	replacesCsvVersion = flag.String("replaces-csv-version", "", "the unified CSV version this new CSV will replace")

	rookContainerImage       = flag.String("rook-image", "", "rook operator container image")
	cephContainerImage       = flag.String("ceph-image", "", "ceph daemon container image")
	rookCsiCephImage         = flag.String("rook-csi-ceph-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiRegistrarImage    = flag.String("rook-csi-registrar-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiResizerImage      = flag.String("rook-csi-resizer-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiProvisionerImage  = flag.String("rook-csi-provisioner-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiSnapshotterImage  = flag.String("rook-csi-snapshotter-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	rookCsiAttacherImage     = flag.String("rook-csi-attacher-image", "", "optional - defaults version supported by rook will be started if this is not set.")
	noobaaContainerImage     = flag.String("noobaa-image", "", "noobaa operator container image")
	noobaaCoreContainerImage = flag.String("noobaa-core-image", "", "noobaa core container image")
	noobaaDBContainerImage   = flag.String("noobaa-db-image", "", "db container image for noobaa")
	ocsContainerImage        = flag.String("ocs-image", "", "ocs operator container image")

	outFile = flag.String("checksum-outfile", "", "the file to write the checksum to")
)

func addArgToHash(key string, val string, md5Hash hash.Hash) hash.Hash {
	fmt.Printf("Adding arg %s to CSV checksum\n", key)
	io.WriteString(md5Hash, fmt.Sprintf("%s=%s", key, val))

	return md5Hash
}

func addFileToHash(filePath string, md5Hash hash.Hash) hash.Hash {
	bytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Adding file %s to CSV checksum\n", filePath)
	io.WriteString(md5Hash, string(bytes))

	return md5Hash
}

func main() {
	flag.Parse()

	if *csvVersion == "" {
		log.Fatal("--csv-version is required")
	} else if *rookContainerImage == "" {
		log.Fatal("--rook-image is required")
	} else if *cephContainerImage == "" {
		log.Fatal("--ceph-image is required")
	} else if *noobaaContainerImage == "" {
		log.Fatal("--noobaa-image is required")
	} else if *noobaaCoreContainerImage == "" {
		log.Fatal("--noobaa-core-image is required")
	} else if *noobaaDBContainerImage == "" {
		log.Fatal("--noobaa-db-image is required")
	} else if *ocsContainerImage == "" {
		log.Fatal("--ocs-image is required")
	} else if *outFile == "" {
		log.Fatal("--checksum-outfile is required")
	}
	// Hash of every .yaml file in the following directories
	// NOTE: directories are not searched recursively
	dirs := []string{
		"deploy",
		"deploy/bundlemanifests",
		"deploy/crds",
		"deploy/olm-catalog/ocs-operator/manifests",
		"deploy/olm-catalog/ocs-operator/metadata",
	}

	md5Hash := md5.New()

	for _, dir := range dirs {
		files, err := ioutil.ReadDir(dir)
		if err != nil {
			panic(err)
		}

		sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

		for _, file := range files {
			// skip directories, non-yaml files, and hidden files
			if file.IsDir() {
				continue
			} else if !strings.Contains(file.Name(), ".yaml") {
				continue
			} else if strings.Contains(file.Name(), "deploy-with-olm.yaml") {
				continue
			} else if strings.HasPrefix(file.Name(), ".") {
				continue
			}
			inputFile := filepath.Join(dir, file.Name())
			md5Hash = addFileToHash(inputFile, md5Hash)
		}
	}

	md5Hash = addArgToHash("--csv-version", *csvVersion, md5Hash)
	md5Hash = addArgToHash("--replaces-csv-version", *replacesCsvVersion, md5Hash)
	md5Hash = addArgToHash("--rook-image", *rookContainerImage, md5Hash)
	md5Hash = addArgToHash("--ceph-image", *cephContainerImage, md5Hash)
	md5Hash = addArgToHash("--rook-csi-ceph-image", *rookCsiCephImage, md5Hash)
	md5Hash = addArgToHash("--rook-csi-registrar-image", *rookCsiRegistrarImage, md5Hash)
	md5Hash = addArgToHash("--rook-csi-resizer-image", *rookCsiResizerImage, md5Hash)
	md5Hash = addArgToHash("--rook-csi-provisioner-image", *rookCsiProvisionerImage, md5Hash)
	md5Hash = addArgToHash("--rook-csi-snapshotter-image", *rookCsiSnapshotterImage, md5Hash)
	md5Hash = addArgToHash("--rook-csi-attacher-image", *rookCsiAttacherImage, md5Hash)
	md5Hash = addArgToHash("--noobaa-image", *noobaaContainerImage, md5Hash)
	md5Hash = addArgToHash("--noobaa-core-image", *noobaaCoreContainerImage, md5Hash)
	md5Hash = addArgToHash("--noobaa-db-image", *noobaaDBContainerImage, md5Hash)
	md5Hash = addArgToHash("--ocs-image", *ocsContainerImage, md5Hash)

	hashInBytes := md5Hash.Sum(nil)
	md5Str := hex.EncodeToString(hashInBytes)

	err := ioutil.WriteFile(*outFile, []byte(md5Str), 0644)
	if err != nil {
		panic(err)
	}

	fmt.Printf("MD5: %s\n", md5Str)
}
