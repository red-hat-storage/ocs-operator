package main

import (
	"flag"
	"os"

	"comparealerts/types"

	"k8s.io/klog/v2"
)

var (
	fileUp   string
	fileDown string

	alertDiffFlagSet = flag.NewFlagSet("Alert Diff", flag.ExitOnError)
)

func init() {
	alertDiffFlagSet.StringVar(&fileUp, "upstream-alert-file", "", "provide an upstream alert file")
	alertDiffFlagSet.StringVar(&fileDown, "downstream-alert-file", "", "provide a downstream alert file")
}

func main() {
	alertDiffFlagSet.Parse(os.Args[1:])
	klog.Info("Upstream:", fileUp)
	klog.Info("Downstream:", fileDown)
	promRuleOne, err := types.NewPrometheusRuleFromFile(fileUp)
	if err != nil {
		klog.Errorf("Err One: %v", err)
		return
	}
	promRuleTwo, err := types.NewPrometheusRuleFromFile(fileDown)
	if err != nil {
		klog.Errorf("Err Two: %v", err)
		return
	}
	diffs := promRuleOne.Diff(promRuleTwo)
	if len(diffs) == 0 {
		klog.Info("No diffs found")
	} else {
		klog.Infof("No: of diffs found: %d", len(diffs))
		for diffIndx, eachDiff := range diffs {
			klog.Infof(">> Diff %d: %+v", diffIndx+1, eachDiff)
			klog.Infoln("\n")
		}
	}
}
