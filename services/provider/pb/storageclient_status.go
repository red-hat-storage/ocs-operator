package providerpb

import (
	cs "github.com/red-hat-storage/ocs-operator/v4/services/provider/clientstatus"
)

// ensure ReportStatusRequest satisfies StorageClientStatus interface
var _ cs.StorageClientStatus = &ReportStatusRequest{}

func (r *ReportStatusRequest) GetPlatformVersion() string {
	return r.ClientPlatformVersion
}

func (r *ReportStatusRequest) GetOperatorVersion() string {
	return r.ClientOperatorVersion
}

func (r *ReportStatusRequest) SetPlatformVersion(version string) cs.StorageClientStatus {
	r.ClientPlatformVersion = version
	return r
}

func (r *ReportStatusRequest) SetOperatorVersion(version string) cs.StorageClientStatus {
	r.ClientOperatorVersion = version
	return r
}
