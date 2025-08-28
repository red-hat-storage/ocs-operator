package providerpb

import (
	ifaces "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/interfaces"
)

// ensure ReportStatusRequest satisfies StorageClientStatus interface
var _ ifaces.StorageClientStatus = &ReportStatusRequest{}

func (r *ReportStatusRequest) SetPlatformVersion(version string) ifaces.StorageClientStatus {
	r.ClientPlatformVersion = version
	return r
}

func (r *ReportStatusRequest) SetOperatorVersion(version string) ifaces.StorageClientStatus {
	r.ClientOperatorVersion = version
	return r
}

func (r *ReportStatusRequest) SetOperatorNamespace(namespace string) ifaces.StorageClientStatus {
	r.ClientOperatorNamespace = namespace
	return r
}

func (r *ReportStatusRequest) SetStorageQuotaUtilizationRatio(storageQuotaUtilizationRatio float64) ifaces.StorageClientStatus {
	r.StorageQuotaUtilizationRatio = storageQuotaUtilizationRatio
	return r
}

func (o *ReportStatusRequest) SetClientOperatorVersion(version string) ifaces.StorageClientInfo {
	o.ClientOperatorVersion = version
	return o
}

func (o *ReportStatusRequest) SetClientPlatformVersion(version string) ifaces.StorageClientInfo {
	o.ClientPlatformVersion = version
	return o
}

func (o *ReportStatusRequest) SetClientOperatorNamespace(name string) ifaces.StorageClientInfo {
	o.ClientOperatorNamespace = name
	return o
}

func (r *ReportStatusRequest) SetClusterID(clusterID string) ifaces.StorageClientInfo {
	r.ClusterID = clusterID
	return r
}

func (r *ReportStatusRequest) SetClientName(clientName string) ifaces.StorageClientInfo {
	r.ClientName = clientName
	return r
}

func (r *ReportStatusRequest) SetClusterName(clusterName string) ifaces.StorageClientInfo {
	r.ClusterName = clusterName
	return r
}

func (r *ReportStatusRequest) SetClientID(clientID string) ifaces.StorageClientInfo {
	r.ClientID = clientID
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

func (o *OnboardConsumerRequest) SetClientOperatorVersion(version string) ifaces.StorageClientInfo {
	o.ClientOperatorVersion = version
	return o
}

func (o *OnboardConsumerRequest) SetClientPlatformVersion(version string) ifaces.StorageClientInfo {
	o.ClientPlatformVersion = version
	return o
}

func (o *OnboardConsumerRequest) SetClientOperatorNamespace(name string) ifaces.StorageClientInfo {
	o.ClientOperatorNamespace = name
	return o
}

func (o *OnboardConsumerRequest) SetClientID(id string) ifaces.StorageClientInfo {
	o.ClientID = id
	return o
}

func (o *OnboardConsumerRequest) SetClientName(name string) ifaces.StorageClientInfo {
	o.ClientName = name
	return o
}

func (o *OnboardConsumerRequest) SetClusterID(id string) ifaces.StorageClientInfo {
	o.ClusterID = id
	return o
}

func (o *OnboardConsumerRequest) SetClusterName(name string) ifaces.StorageClientInfo {
	o.ClusterName = name
	return o
}
