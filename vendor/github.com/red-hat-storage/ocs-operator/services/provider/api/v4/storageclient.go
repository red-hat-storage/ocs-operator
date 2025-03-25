package providerpb

import (
	ifaces "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/interfaces"
)

// ensure ReportStatusRequest satisfies StorageClientStatus interface
var _ ifaces.StorageClientStatus = &ReportStatusRequest{}

func (r *ReportStatusRequest) GetPlatformVersion() string {
	return r.GetClientPlatformVersion()
}

func (r *ReportStatusRequest) GetOperatorVersion() string {
	return r.GetClientOperatorVersion()
}

func (r *ReportStatusRequest) GetOperatorNamespace() string {
	return r.GetClientOperatorNamespace()
}

func (r *ReportStatusRequest) SetPlatformVersion(version string) ifaces.StorageClientStatus {
	r.ClientPlatformVersion = version
	return r
}

func (r *ReportStatusRequest) SetOperatorVersion(version string) ifaces.StorageClientStatus {
	r.ClientOperatorVersion = version
	return r
}

func (r *ReportStatusRequest) SetClusterID(clusterID string) ifaces.StorageClientStatus {
	r.ClusterID = clusterID
	return r
}

func (r *ReportStatusRequest) SetClientName(clientName string) ifaces.StorageClientStatus {
	r.ClientName = clientName
	return r
}

func (r *ReportStatusRequest) SetClusterName(clusterName string) ifaces.StorageClientStatus {
	r.ClusterName = clusterName
	return r
}

func (r *ReportStatusRequest) SetClientID(clientID string) ifaces.StorageClientStatus {
	r.ClientID = clientID
	return r
}

func (r *ReportStatusRequest) SetStorageQuotaUtilizationRatio(storageQuotaUtilizationRatio float64) ifaces.StorageClientStatus {
	r.StorageQuotaUtilizationRatio = storageQuotaUtilizationRatio
	return r
}

func (r *ReportStatusRequest) SetOperatorNamespace(namespace string) ifaces.StorageClientStatus {
	r.ClientOperatorNamespace = namespace
	return r
}

// ensure OnboardConsumerRequest satisfies StorageClientOnboarding interface
var _ ifaces.StorageClientOnboarding = &OnboardConsumerRequest{}

func (o *OnboardConsumerRequest) SetOnboardingTicket(ticket string) ifaces.StorageClientOnboarding {
	o.OnboardingTicket = ticket
	return o
}

func (o *OnboardConsumerRequest) SetConsumerName(name string) ifaces.StorageClientOnboarding {
	o.ConsumerName = name
	return o
}

func (o *OnboardConsumerRequest) SetClientOperatorVersion(version string) ifaces.StorageClientOnboarding {
	o.ClientOperatorVersion = version
	return o
}
