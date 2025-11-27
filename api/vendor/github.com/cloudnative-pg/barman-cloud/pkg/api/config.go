/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// +genclient
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen=package

package api

import (
	"slices"
	"strings"

	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// EncryptionType encapsulated the available types of encryption
type EncryptionType string

const (
	// EncryptionTypeNone means just use the bucket configuration
	EncryptionTypeNone = EncryptionType("")

	// EncryptionTypeAES256 means to use AES256 encryption
	EncryptionTypeAES256 = EncryptionType("AES256")

	// EncryptionTypeNoneAWSKMS means to use aws:kms encryption
	EncryptionTypeNoneAWSKMS = EncryptionType("aws:kms")
)

// CompressionType encapsulates the available types of compression
type CompressionType string

const (
	// CompressionTypeNone means no compression is performed
	CompressionTypeNone = CompressionType("")

	// CompressionTypeGzip means gzip compression is performed
	CompressionTypeGzip = CompressionType("gzip")

	// CompressionTypeBzip2 means bzip2 compression is performed
	CompressionTypeBzip2 = CompressionType("bzip2")

	// CompressionTypeLz4 means lz4 compression is performed
	CompressionTypeLz4 = CompressionType("lz4")

	// CompressionTypeSnappy means snappy compression is performed
	CompressionTypeSnappy = CompressionType("snappy")

	// CompressionTypeXz means xz compression is performed
	CompressionTypeXz = CompressionType("xz")

	// CompressionTypeZstd means zstd compression is performed
	CompressionTypeZstd = CompressionType("zstd")
)

// BarmanCredentials an object containing the potential credentials for each cloud provider
type BarmanCredentials struct {
	// The credentials to use to upload data to Google Cloud Storage
	// +optional
	Google *GoogleCredentials `json:"googleCredentials,omitempty"`

	// The credentials to use to upload data to S3
	// +optional
	AWS *S3Credentials `json:"s3Credentials,omitempty"`

	// The credentials to use to upload data to Azure Blob Storage
	// +optional
	Azure *AzureCredentials `json:"azureCredentials,omitempty"`
}

// S3Credentials is the type for the credentials to be used to upload
// files to S3. It can be provided in two alternative ways:
//
// - explicitly passing accessKeyId and secretAccessKey
//
// - inheriting the role from the pod environment by setting inheritFromIAMRole to true
type S3Credentials struct {
	// The reference to the access key id
	// +optional
	AccessKeyIDReference *machineryapi.SecretKeySelector `json:"accessKeyId,omitempty"`

	// The reference to the secret access key
	// +optional
	SecretAccessKeyReference *machineryapi.SecretKeySelector `json:"secretAccessKey,omitempty"`

	// The reference to the secret containing the region name
	// +optional
	RegionReference *machineryapi.SecretKeySelector `json:"region,omitempty"`

	// The references to the session key
	// +optional
	SessionToken *machineryapi.SecretKeySelector `json:"sessionToken,omitempty"`

	// Use the role based authentication without providing explicitly the keys.
	// +optional
	InheritFromIAMRole bool `json:"inheritFromIAMRole,omitempty"`
}

// AzureCredentials is the type for the credentials to be used to upload
// files to Azure Blob Storage. The connection string contains every needed
// information. If the connection string is not specified, we'll need the
// storage account name and also one (and only one) of:
//
// - storageKey
// - storageSasToken
//
// - inheriting the credentials from the pod environment by setting inheritFromAzureAD to true
type AzureCredentials struct {
	// The connection string to be used
	// +optional
	ConnectionString *machineryapi.SecretKeySelector `json:"connectionString,omitempty"`

	// The storage account where to upload data
	// +optional
	StorageAccount *machineryapi.SecretKeySelector `json:"storageAccount,omitempty"`

	// The storage account key to be used in conjunction
	// with the storage account name
	// +optional
	StorageKey *machineryapi.SecretKeySelector `json:"storageKey,omitempty"`

	// A shared-access-signature to be used in conjunction with
	// the storage account name
	// +optional
	StorageSasToken *machineryapi.SecretKeySelector `json:"storageSasToken,omitempty"`

	// Use the Azure AD based authentication without providing explicitly the keys.
	// +optional
	InheritFromAzureAD bool `json:"inheritFromAzureAD,omitempty"`
}

// GoogleCredentials is the type for the Google Cloud Storage credentials.
// This needs to be specified even if we run inside a GKE environment.
type GoogleCredentials struct {
	// The secret containing the Google Cloud Storage JSON file with the credentials
	// +optional
	ApplicationCredentials *machineryapi.SecretKeySelector `json:"applicationCredentials,omitempty"`

	// If set to true, will presume that it's running inside a GKE environment,
	// default to false.
	// +optional
	GKEEnvironment bool `json:"gkeEnvironment,omitempty"`
}

// BarmanObjectStoreConfiguration contains the backup configuration
// using Barman against an S3-compatible object storage
type BarmanObjectStoreConfiguration struct {
	// The potential credentials for each cloud provider
	BarmanCredentials `json:",inline"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	// +optional
	EndpointURL string `json:"endpointURL,omitempty"`

	// EndpointCA store the CA bundle of the barman endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and barman-cloud-wal-archive
	// +optional
	EndpointCA *machineryapi.SecretKeySelector `json:"endpointCA,omitempty"`

	// The path where to store the backup (i.e. s3://bucket/path/to/folder)
	// this path, with different destination folders, will be used for WALs
	// and for data
	// +kubebuilder:validation:MinLength=1
	DestinationPath string `json:"destinationPath"`

	// The server name on S3, the cluster name is used if this
	// parameter is omitted
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// The configuration for the backup of the WAL stream.
	// When not defined, WAL files will be stored uncompressed and may be
	// unencrypted in the object store, according to the bucket default policy.
	// +optional
	Wal *WalBackupConfiguration `json:"wal,omitempty"`

	// The configuration to be used to backup the data files
	// When not defined, base backups files will be stored uncompressed and may
	// be unencrypted in the object store, according to the bucket default
	// policy.
	// +optional
	Data *DataBackupConfiguration `json:"data,omitempty"`

	// Tags is a list of key value pairs that will be passed to the
	// Barman --tags option.
	// +optional
	Tags map[string]string `json:"tags,omitempty"`

	// HistoryTags is a list of key value pairs that will be passed to the
	// Barman --history-tags option.
	// +optional
	HistoryTags map[string]string `json:"historyTags,omitempty"`
}

// WalBackupConfiguration is the configuration of the backup of the
// WAL stream
type WalBackupConfiguration struct {
	// Compress a WAL file before sending it to the object store. Available
	// options are empty string (no compression, default), `gzip`, `bzip2`,
	// `lz4`, `snappy`, `xz`, and `zstd`.
	// +kubebuilder:validation:Enum=bzip2;gzip;lz4;snappy;xz;zstd
	// +optional
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	// +kubebuilder:validation:Enum=AES256;"aws:kms"
	// +optional
	Encryption EncryptionType `json:"encryption,omitempty"`

	// Number of WAL files to be either archived in parallel (when the
	// PostgreSQL instance is archiving to a backup object store) or
	// restored in parallel (when a PostgreSQL standby is fetching WAL
	// files from a recovery object store). If not specified, WAL files
	// will be processed one at a time. It accepts a positive integer as a
	// value - with 1 being the minimum accepted value.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxParallel int `json:"maxParallel,omitempty"`
	// Additional arguments that can be appended to the 'barman-cloud-wal-archive'
	// command-line invocation. These arguments provide flexibility to customize
	// the WAL archive process further, according to specific requirements or configurations.
	//
	// Example:
	// In a scenario where specialized backup options are required, such as setting
	// a specific timeout or defining custom behavior, users can use this field
	// to specify additional command arguments.
	//
	// Note:
	// It's essential to ensure that the provided arguments are valid and supported
	// by the 'barman-cloud-wal-archive' command, to avoid potential errors or unintended
	// behavior during execution.
	// +optional
	ArchiveAdditionalCommandArgs []string `json:"archiveAdditionalCommandArgs,omitempty"`

	// Additional arguments that can be appended to the 'barman-cloud-wal-restore'
	// command-line invocation. These arguments provide flexibility to customize
	// the WAL restore process further, according to specific requirements or configurations.
	//
	// Example:
	// In a scenario where specialized backup options are required, such as setting
	// a specific timeout or defining custom behavior, users can use this field
	// to specify additional command arguments.
	//
	// Note:
	// It's essential to ensure that the provided arguments are valid and supported
	// by the 'barman-cloud-wal-restore' command, to avoid potential errors or unintended
	// behavior during execution.
	// +optional
	RestoreAdditionalCommandArgs []string `json:"restoreAdditionalCommandArgs,omitempty"`
}

// DataBackupConfiguration is the configuration of the backup of
// the data directory
type DataBackupConfiguration struct {
	// Compress a backup file (a tar file per tablespace) while streaming it
	// to the object store. Available options are empty string (no
	// compression, default), `gzip`, `bzip2`, and `snappy`.
	// +kubebuilder:validation:Enum=bzip2;gzip;snappy
	// +optional
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	// +kubebuilder:validation:Enum=AES256;"aws:kms"
	// +optional
	Encryption EncryptionType `json:"encryption,omitempty"`

	// The number of parallel jobs to be used to upload the backup, defaults
	// to 2
	// +kubebuilder:validation:Minimum=1
	// +optional
	Jobs *int32 `json:"jobs,omitempty"`

	// Control whether the I/O workload for the backup initial checkpoint will
	// be limited, according to the `checkpoint_completion_target` setting on
	// the PostgreSQL server. If set to true, an immediate checkpoint will be
	// used, meaning PostgreSQL will complete the checkpoint as soon as
	// possible. `false` by default.
	// +optional
	ImmediateCheckpoint bool `json:"immediateCheckpoint,omitempty"`

	// AdditionalCommandArgs represents additional arguments that can be appended
	// to the 'barman-cloud-backup' command-line invocation. These arguments
	// provide flexibility to customize the backup process further according to
	// specific requirements or configurations.
	//
	// Example:
	// In a scenario where specialized backup options are required, such as setting
	// a specific timeout or defining custom behavior, users can use this field
	// to specify additional command arguments.
	//
	// Note:
	// It's essential to ensure that the provided arguments are valid and supported
	// by the 'barman-cloud-backup' command, to avoid potential errors or unintended
	// behavior during execution.
	// +optional
	AdditionalCommandArgs []string `json:"additionalCommandArgs,omitempty"`
}

// ArePopulated checks if the passed set of credentials contains
// something
func (crendentials BarmanCredentials) ArePopulated() bool {
	return crendentials.Azure != nil || crendentials.AWS != nil || crendentials.Google != nil
}

// ValidateAzureCredentials checks and validates the azure credentials
func (azure *AzureCredentials) ValidateAzureCredentials(path *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	secrets := 0
	if azure.InheritFromAzureAD {
		secrets++
	}
	if azure.StorageKey != nil {
		secrets++
	}
	if azure.StorageSasToken != nil {
		secrets++
	}

	if secrets != 1 && azure.ConnectionString == nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				azure,
				"when connection string is not specified, one and only one of "+
					"storage key and storage SAS token is allowed"))
	}

	if secrets != 0 && azure.ConnectionString != nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				azure,
				"when connection string is specified, the other parameters "+
					"must be empty"))
	}

	return allErrors
}

// ValidateAwsCredentials validates the AWS Credentials
func (s3 *S3Credentials) ValidateAwsCredentials(path *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}
	credentials := 0

	if s3.InheritFromIAMRole {
		credentials++
	}
	if s3.AccessKeyIDReference != nil && s3.SecretAccessKeyReference != nil {
		credentials++
	} else if s3.AccessKeyIDReference != nil || s3.SecretAccessKeyReference != nil {
		credentials++
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				s3,
				"when using AWS credentials both accessKeyId and secretAccessKey must be provided",
			),
		)
	}

	if credentials == 0 {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				s3,
				"at least one AWS authentication method should be supplied",
			),
		)
	}

	if credentials > 1 {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				s3,
				"only one AWS authentication method should be supplied",
			),
		)
	}

	return allErrors
}

// ValidateGCSCredentials validates the GCS credentials
func (gcs *GoogleCredentials) ValidateGCSCredentials(path *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	if !gcs.GKEEnvironment && gcs.ApplicationCredentials == nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				gcs,
				"if gkeEnvironment is false, secret with credentials must be provided",
			))
	}

	if gcs.GKEEnvironment && gcs.ApplicationCredentials != nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				gcs,
				"if gkeEnvironment is true, secret with credentials must not be provided",
			))
	}

	return allErrors
}

// AppendAdditionalCommandArgs adds custom arguments as barman-cloud-backup command-line options
func (cfg *DataBackupConfiguration) AppendAdditionalCommandArgs(options []string) []string {
	if cfg == nil || len(cfg.AdditionalCommandArgs) == 0 {
		return options
	}
	return appendAdditionalCommandArgs(cfg.AdditionalCommandArgs, options)
}

// AppendArchiveAdditionalCommandArgs adds custom arguments as barman-cloud-wal-archive command-line options
func (cfg *WalBackupConfiguration) AppendArchiveAdditionalCommandArgs(options []string) []string {
	if cfg == nil || len(cfg.ArchiveAdditionalCommandArgs) == 0 {
		return options
	}
	return appendAdditionalCommandArgs(cfg.ArchiveAdditionalCommandArgs, options)
}

// AppendRestoreAdditionalCommandArgs adds custom arguments as barman-cloud-wal-restore command-line options
func (cfg *WalBackupConfiguration) AppendRestoreAdditionalCommandArgs(options []string) []string {
	if cfg == nil || len(cfg.RestoreAdditionalCommandArgs) == 0 {
		return options
	}
	return appendAdditionalCommandArgs(cfg.RestoreAdditionalCommandArgs, options)
}

func appendAdditionalCommandArgs(additionalCommandArgs []string, options []string) []string {
	optionKeys := map[string]bool{}
	for _, option := range options {
		key := strings.Split(option, "=")[0]
		if key != "" {
			optionKeys[key] = true
		}
	}
	for _, additionalCommandArg := range additionalCommandArgs {
		key := strings.Split(additionalCommandArg, "=")[0]
		if key == "" || slices.Contains(options, key) || optionKeys[key] {
			continue
		}
		options = append(options, additionalCommandArg)
	}
	return options
}
