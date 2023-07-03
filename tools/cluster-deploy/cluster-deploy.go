package main

import (
	"flag"
	"log"
	"os"

	deploymanager "github.com/red-hat-storage/ocs-operator/v4/pkg/deploy-manager"
)

var (
	ocsCatalogSourceImage  = flag.String("ocs-catalog-image", "", "The OCS CatalogSource container image to use in the deployment")
	ocsSubscriptionChannel = flag.String("ocs-subscription-channel", "", "The subscription channel to receive upgrades from in the deployment")
	installNamespace       = flag.String("install-namespace", "openshift-storage", "The namespace the operator runs in")
	yamlOutputPath         = flag.String("yaml-output-path", "", "Just generate the yaml for the OCS olm deployment and dump it to a file")
	arbiterEnabled         = flag.Bool("arbiter", false, "Deploy the StorageCluster with arbiter enabled")
)

func main() {

	flag.Parse()
	if *ocsCatalogSourceImage == "" {
		log.Fatal("--ocs-catalog-image is required")
	} else if *ocsSubscriptionChannel == "" {
		log.Fatal("--ocs-subscription-channel is required")
	} else if *installNamespace == "" {
		log.Fatal("--install-namespace is required")
	}

	t, _ := deploymanager.NewDeployManager()

	t.SetNamespace(*installNamespace)

	if *arbiterEnabled {
		t.EnableArbiter()
	}

	if *yamlOutputPath != "" {
		yaml := t.DumpYAML(*ocsCatalogSourceImage, *ocsSubscriptionChannel)
		err := os.WriteFile(*yamlOutputPath, []byte(yaml), 0644)
		if err != nil {
			panic(err)
		}
		os.Exit(0)
	}

	log.Printf("Deploying ocs image %s", *ocsCatalogSourceImage)
	err := t.DeployOCSWithOLM(*ocsCatalogSourceImage, *ocsSubscriptionChannel)
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
