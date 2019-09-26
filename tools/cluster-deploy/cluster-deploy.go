package main

import (
	"flag"
	"log"

	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
)

var (
	ocsRegistryImage          = flag.String("ocs-registry-image", "", "The ocs-registry container image to use in the deployment")
	localStorageRegistryImage = flag.String("local-storage-registry-image", "", "The local storage registry image to use in the deployment")
)

func main() {

	flag.Parse()
	if *ocsRegistryImage == "" {
		log.Fatal("--ocs-registry-image is required")
	} else if *localStorageRegistryImage == "" {
		log.Fatal("--local-storage-registry-image is required")
	}

	t, err := deploymanager.NewDeployManager()

	log.Printf("Deploying ocs image %s", *ocsRegistryImage)
	err = t.DeployOCSWithOLM(*ocsRegistryImage, *localStorageRegistryImage)
	if err != nil {
		panic(err)
	}

	log.Printf("Waiting for ocs-operator to come online")
	err = t.WaitForOCSOperator()
	if err != nil {
		panic(err)
	}

	log.Printf("Starting default storage cluster")
	err = t.StartDefaultStorageCluster()
	if err != nil {
		panic(err)
	}

	log.Printf("Deployment successful")
}
