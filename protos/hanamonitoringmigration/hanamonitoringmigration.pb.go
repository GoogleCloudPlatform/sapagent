//
//Copyright 2022 Google LLC
//
//Licensed under the Apache License, Version 2.0 (the "License");
//you may not use this file except in compliance with the License.
//You may obtain a copy of the License at
//
//https://www.apache.org/licenses/LICENSE-2.0
//
//Unless required by applicable law or agreed to in writing, software
//distributed under the License is distributed on an "AS IS" BASIS,
//WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//See the License for the specific language governing permissions and
//limitations under the License.

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.31.0
// 	protoc        v3.6.1
// source: hanamonitoringmigration/hanamonitoringmigration.proto

package hanamonitoringmigration

import (
	instanceinfo "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type MetricType int32

const (
	MetricType_METRIC_UNSPECIFIED MetricType = 0
	MetricType_LABEL              MetricType = 1
	MetricType_GAUGE              MetricType = 2
	MetricType_CUMULATIVE         MetricType = 3
	MetricType_DELTA              MetricType = 4
)

// Enum value maps for MetricType.
var (
	MetricType_name = map[int32]string{
		0: "METRIC_UNSPECIFIED",
		1: "LABEL",
		2: "GAUGE",
		3: "CUMULATIVE",
		4: "DELTA",
	}
	MetricType_value = map[string]int32{
		"METRIC_UNSPECIFIED": 0,
		"LABEL":              1,
		"GAUGE":              2,
		"CUMULATIVE":         3,
		"DELTA":              4,
	}
)

func (x MetricType) Enum() *MetricType {
	p := new(MetricType)
	*p = x
	return p
}

func (x MetricType) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (MetricType) Descriptor() protoreflect.EnumDescriptor {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_enumTypes[0].Descriptor()
}

func (MetricType) Type() protoreflect.EnumType {
	return &file_hanamonitoringmigration_hanamonitoringmigration_proto_enumTypes[0]
}

func (x MetricType) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use MetricType.Descriptor instead.
func (MetricType) EnumDescriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{0}
}

type ValueType int32

const (
	ValueType_VALUE_UNSPECIFIED ValueType = 0
	ValueType_BOOL              ValueType = 1
	ValueType_INT64             ValueType = 2
	ValueType_STRING            ValueType = 3
	ValueType_DOUBLE            ValueType = 4
	ValueType_DISTRIBUTION      ValueType = 5
)

// Enum value maps for ValueType.
var (
	ValueType_name = map[int32]string{
		0: "VALUE_UNSPECIFIED",
		1: "BOOL",
		2: "INT64",
		3: "STRING",
		4: "DOUBLE",
		5: "DISTRIBUTION",
	}
	ValueType_value = map[string]int32{
		"VALUE_UNSPECIFIED": 0,
		"BOOL":              1,
		"INT64":             2,
		"STRING":            3,
		"DOUBLE":            4,
		"DISTRIBUTION":      5,
	}
)

func (x ValueType) Enum() *ValueType {
	p := new(ValueType)
	*p = x
	return p
}

func (x ValueType) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (ValueType) Descriptor() protoreflect.EnumDescriptor {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_enumTypes[1].Descriptor()
}

func (ValueType) Type() protoreflect.EnumType {
	return &file_hanamonitoringmigration_hanamonitoringmigration_proto_enumTypes[1]
}

func (x ValueType) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use ValueType.Descriptor instead.
func (ValueType) EnumDescriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{1}
}

type HANAMonitoringConfiguration struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Agent   *AgentConfig `protobuf:"bytes,1,opt,name=agent,proto3" json:"agent,omitempty"`
	Queries []*Query     `protobuf:"bytes,2,rep,name=queries,proto3" json:"queries,omitempty"`
	Enabled bool         `protobuf:"varint,3,opt,name=enabled,proto3" json:"enabled,omitempty"`
}

func (x *HANAMonitoringConfiguration) Reset() {
	*x = HANAMonitoringConfiguration{}
	if protoimpl.UnsafeEnabled {
		mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *HANAMonitoringConfiguration) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*HANAMonitoringConfiguration) ProtoMessage() {}

func (x *HANAMonitoringConfiguration) ProtoReflect() protoreflect.Message {
	mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use HANAMonitoringConfiguration.ProtoReflect.Descriptor instead.
func (*HANAMonitoringConfiguration) Descriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{0}
}

func (x *HANAMonitoringConfiguration) GetAgent() *AgentConfig {
	if x != nil {
		return x.Agent
	}
	return nil
}

func (x *HANAMonitoringConfiguration) GetQueries() []*Query {
	if x != nil {
		return x.Queries
	}
	return nil
}

func (x *HANAMonitoringConfiguration) GetEnabled() bool {
	if x != nil {
		return x.Enabled
	}
	return false
}

type AgentConfig struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	SampleInterval   int64                         `protobuf:"varint,1,opt,name=sample_interval,json=sampleInterval,proto3" json:"sample_interval,omitempty"`
	QueryTimeout     int64                         `protobuf:"varint,2,opt,name=query_timeout,json=queryTimeout,proto3" json:"query_timeout,omitempty"`
	ExecutionThreads int64                         `protobuf:"varint,3,opt,name=execution_threads,json=executionThreads,proto3" json:"execution_threads,omitempty"`
	CloudProperties  *instanceinfo.CloudProperties `protobuf:"bytes,4,opt,name=cloud_properties,json=cloudProperties,proto3" json:"cloud_properties,omitempty"`
	GcloudAuth       *GcloudAuth                   `protobuf:"bytes,5,opt,name=gcloud_auth,json=gcloudAuth,proto3" json:"gcloud_auth,omitempty"`
	HanaInstances    []*HANAInstance               `protobuf:"bytes,6,rep,name=hana_instances,json=hanaInstances,proto3" json:"hana_instances,omitempty"`
}

func (x *AgentConfig) Reset() {
	*x = AgentConfig{}
	if protoimpl.UnsafeEnabled {
		mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *AgentConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AgentConfig) ProtoMessage() {}

func (x *AgentConfig) ProtoReflect() protoreflect.Message {
	mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AgentConfig.ProtoReflect.Descriptor instead.
func (*AgentConfig) Descriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{1}
}

func (x *AgentConfig) GetSampleInterval() int64 {
	if x != nil {
		return x.SampleInterval
	}
	return 0
}

func (x *AgentConfig) GetQueryTimeout() int64 {
	if x != nil {
		return x.QueryTimeout
	}
	return 0
}

func (x *AgentConfig) GetExecutionThreads() int64 {
	if x != nil {
		return x.ExecutionThreads
	}
	return 0
}

func (x *AgentConfig) GetCloudProperties() *instanceinfo.CloudProperties {
	if x != nil {
		return x.CloudProperties
	}
	return nil
}

func (x *AgentConfig) GetGcloudAuth() *GcloudAuth {
	if x != nil {
		return x.GcloudAuth
	}
	return nil
}

func (x *AgentConfig) GetHanaInstances() []*HANAInstance {
	if x != nil {
		return x.HanaInstances
	}
	return nil
}

type GcloudAuth struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	ServiceAccountJson string `protobuf:"bytes,1,opt,name=service_account_json,json=serviceAccountJson,proto3" json:"service_account_json,omitempty"`
}

func (x *GcloudAuth) Reset() {
	*x = GcloudAuth{}
	if protoimpl.UnsafeEnabled {
		mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GcloudAuth) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GcloudAuth) ProtoMessage() {}

func (x *GcloudAuth) ProtoReflect() protoreflect.Message {
	mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GcloudAuth.ProtoReflect.Descriptor instead.
func (*GcloudAuth) Descriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{2}
}

func (x *GcloudAuth) GetServiceAccountJson() string {
	if x != nil {
		return x.ServiceAccountJson
	}
	return ""
}

type HANAInstance struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Name                  string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Sid                   string `protobuf:"bytes,2,opt,name=sid,proto3" json:"sid,omitempty"`
	Host                  string `protobuf:"bytes,3,opt,name=host,proto3" json:"host,omitempty"`
	Port                  int64  `protobuf:"varint,4,opt,name=port,proto3" json:"port,omitempty"`
	User                  string `protobuf:"bytes,5,opt,name=user,proto3" json:"user,omitempty"`
	Password              string `protobuf:"bytes,6,opt,name=password,proto3" json:"password,omitempty"`
	SecretName            string `protobuf:"bytes,7,opt,name=secret_name,json=secretName,proto3" json:"secret_name,omitempty"`
	EnableSsl             bool   `protobuf:"varint,8,opt,name=enable_ssl,json=enableSsl,proto3" json:"enable_ssl,omitempty"`
	ValidateCertificate   bool   `protobuf:"varint,9,opt,name=validate_certificate,json=validateCertificate,proto3" json:"validate_certificate,omitempty"`
	HostNameInCertificate string `protobuf:"bytes,10,opt,name=host_name_in_certificate,json=hostNameInCertificate,proto3" json:"host_name_in_certificate,omitempty"`
	Connections           int64  `protobuf:"varint,11,opt,name=connections,proto3" json:"connections,omitempty"`
	KeyStore              string `protobuf:"bytes,12,opt,name=key_store,json=keyStore,proto3" json:"key_store,omitempty"`
	KeyStorePassword      string `protobuf:"bytes,13,opt,name=key_store_password,json=keyStorePassword,proto3" json:"key_store_password,omitempty"`
	KeyStoreSecretName    string `protobuf:"bytes,14,opt,name=key_store_secret_name,json=keyStoreSecretName,proto3" json:"key_store_secret_name,omitempty"`
	TrustStore            string `protobuf:"bytes,15,opt,name=trust_store,json=trustStore,proto3" json:"trust_store,omitempty"`
	TrustStorePassword    string `protobuf:"bytes,16,opt,name=trust_store_password,json=trustStorePassword,proto3" json:"trust_store_password,omitempty"`
	TrustStoreSecretName  string `protobuf:"bytes,17,opt,name=trust_store_secret_name,json=trustStoreSecretName,proto3" json:"trust_store_secret_name,omitempty"`
}

func (x *HANAInstance) Reset() {
	*x = HANAInstance{}
	if protoimpl.UnsafeEnabled {
		mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *HANAInstance) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*HANAInstance) ProtoMessage() {}

func (x *HANAInstance) ProtoReflect() protoreflect.Message {
	mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use HANAInstance.ProtoReflect.Descriptor instead.
func (*HANAInstance) Descriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{3}
}

func (x *HANAInstance) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *HANAInstance) GetSid() string {
	if x != nil {
		return x.Sid
	}
	return ""
}

func (x *HANAInstance) GetHost() string {
	if x != nil {
		return x.Host
	}
	return ""
}

func (x *HANAInstance) GetPort() int64 {
	if x != nil {
		return x.Port
	}
	return 0
}

func (x *HANAInstance) GetUser() string {
	if x != nil {
		return x.User
	}
	return ""
}

func (x *HANAInstance) GetPassword() string {
	if x != nil {
		return x.Password
	}
	return ""
}

func (x *HANAInstance) GetSecretName() string {
	if x != nil {
		return x.SecretName
	}
	return ""
}

func (x *HANAInstance) GetEnableSsl() bool {
	if x != nil {
		return x.EnableSsl
	}
	return false
}

func (x *HANAInstance) GetValidateCertificate() bool {
	if x != nil {
		return x.ValidateCertificate
	}
	return false
}

func (x *HANAInstance) GetHostNameInCertificate() string {
	if x != nil {
		return x.HostNameInCertificate
	}
	return ""
}

func (x *HANAInstance) GetConnections() int64 {
	if x != nil {
		return x.Connections
	}
	return 0
}

func (x *HANAInstance) GetKeyStore() string {
	if x != nil {
		return x.KeyStore
	}
	return ""
}

func (x *HANAInstance) GetKeyStorePassword() string {
	if x != nil {
		return x.KeyStorePassword
	}
	return ""
}

func (x *HANAInstance) GetKeyStoreSecretName() string {
	if x != nil {
		return x.KeyStoreSecretName
	}
	return ""
}

func (x *HANAInstance) GetTrustStore() string {
	if x != nil {
		return x.TrustStore
	}
	return ""
}

func (x *HANAInstance) GetTrustStorePassword() string {
	if x != nil {
		return x.TrustStorePassword
	}
	return ""
}

func (x *HANAInstance) GetTrustStoreSecretName() string {
	if x != nil {
		return x.TrustStoreSecretName
	}
	return ""
}

type Query struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Enabled        bool      `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	Name           string    `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Description    string    `protobuf:"bytes,3,opt,name=description,proto3" json:"description,omitempty"`
	Sql            string    `protobuf:"bytes,4,opt,name=sql,proto3" json:"sql,omitempty"`
	SampleInterval int64     `protobuf:"varint,5,opt,name=sample_interval,json=sampleInterval,proto3" json:"sample_interval,omitempty"`
	Columns        []*Column `protobuf:"bytes,6,rep,name=columns,proto3" json:"columns,omitempty"`
	Curated        bool      `protobuf:"varint,7,opt,name=curated,proto3" json:"curated,omitempty"`
}

func (x *Query) Reset() {
	*x = Query{}
	if protoimpl.UnsafeEnabled {
		mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Query) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Query) ProtoMessage() {}

func (x *Query) ProtoReflect() protoreflect.Message {
	mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Query.ProtoReflect.Descriptor instead.
func (*Query) Descriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{4}
}

func (x *Query) GetEnabled() bool {
	if x != nil {
		return x.Enabled
	}
	return false
}

func (x *Query) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *Query) GetDescription() string {
	if x != nil {
		return x.Description
	}
	return ""
}

func (x *Query) GetSql() string {
	if x != nil {
		return x.Sql
	}
	return ""
}

func (x *Query) GetSampleInterval() int64 {
	if x != nil {
		return x.SampleInterval
	}
	return 0
}

func (x *Query) GetColumns() []*Column {
	if x != nil {
		return x.Columns
	}
	return nil
}

func (x *Query) GetCurated() bool {
	if x != nil {
		return x.Curated
	}
	return false
}

type Column struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Name         string     `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Description  string     `protobuf:"bytes,2,opt,name=description,proto3" json:"description,omitempty"`
	Units        string     `protobuf:"bytes,3,opt,name=units,proto3" json:"units,omitempty"`
	MetricType   MetricType `protobuf:"varint,4,opt,name=metric_type,json=metricType,proto3,enum=sapagent.protos.hanamonitoringmigration.MetricType" json:"metric_type,omitempty"`
	ValueType    ValueType  `protobuf:"varint,5,opt,name=value_type,json=valueType,proto3,enum=sapagent.protos.hanamonitoringmigration.ValueType" json:"value_type,omitempty"`
	NameOverride string     `protobuf:"bytes,6,opt,name=name_override,json=nameOverride,proto3" json:"name_override,omitempty"`
}

func (x *Column) Reset() {
	*x = Column{}
	if protoimpl.UnsafeEnabled {
		mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[5]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Column) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Column) ProtoMessage() {}

func (x *Column) ProtoReflect() protoreflect.Message {
	mi := &file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[5]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Column.ProtoReflect.Descriptor instead.
func (*Column) Descriptor() ([]byte, []int) {
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP(), []int{5}
}

func (x *Column) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *Column) GetDescription() string {
	if x != nil {
		return x.Description
	}
	return ""
}

func (x *Column) GetUnits() string {
	if x != nil {
		return x.Units
	}
	return ""
}

func (x *Column) GetMetricType() MetricType {
	if x != nil {
		return x.MetricType
	}
	return MetricType_METRIC_UNSPECIFIED
}

func (x *Column) GetValueType() ValueType {
	if x != nil {
		return x.ValueType
	}
	return ValueType_VALUE_UNSPECIFIED
}

func (x *Column) GetNameOverride() string {
	if x != nil {
		return x.NameOverride
	}
	return ""
}

var File_hanamonitoringmigration_hanamonitoringmigration_proto protoreflect.FileDescriptor

var file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDesc = []byte{
	0x0a, 0x35, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67,
	0x6d, 0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2f, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f,
	0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d, 0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f,
	0x6e, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x27, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e,
	0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f, 0x6e,
	0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d, 0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e,
	0x1a, 0x1f, 0x69, 0x6e, 0x73, 0x74, 0x61, 0x6e, 0x63, 0x65, 0x69, 0x6e, 0x66, 0x6f, 0x2f, 0x69,
	0x6e, 0x73, 0x74, 0x61, 0x6e, 0x63, 0x65, 0x69, 0x6e, 0x66, 0x6f, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x22, 0xcd, 0x01, 0x0a, 0x1b, 0x48, 0x41, 0x4e, 0x41, 0x4d, 0x6f, 0x6e, 0x69, 0x74, 0x6f,
	0x72, 0x69, 0x6e, 0x67, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x75, 0x72, 0x61, 0x74, 0x69, 0x6f,
	0x6e, 0x12, 0x4a, 0x0a, 0x05, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b,
	0x32, 0x34, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x73, 0x2e, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e,
	0x67, 0x6d, 0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x41, 0x67, 0x65, 0x6e, 0x74,
	0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x52, 0x05, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x12, 0x48, 0x0a,
	0x07, 0x71, 0x75, 0x65, 0x72, 0x69, 0x65, 0x73, 0x18, 0x02, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x2e,
	0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73,
	0x2e, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d,
	0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x51, 0x75, 0x65, 0x72, 0x79, 0x52, 0x07,
	0x71, 0x75, 0x65, 0x72, 0x69, 0x65, 0x73, 0x12, 0x18, 0x0a, 0x07, 0x65, 0x6e, 0x61, 0x62, 0x6c,
	0x65, 0x64, 0x18, 0x03, 0x20, 0x01, 0x28, 0x08, 0x52, 0x07, 0x65, 0x6e, 0x61, 0x62, 0x6c, 0x65,
	0x64, 0x22, 0x96, 0x03, 0x0a, 0x0b, 0x41, 0x67, 0x65, 0x6e, 0x74, 0x43, 0x6f, 0x6e, 0x66, 0x69,
	0x67, 0x12, 0x27, 0x0a, 0x0f, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x5f, 0x69, 0x6e, 0x74, 0x65,
	0x72, 0x76, 0x61, 0x6c, 0x18, 0x01, 0x20, 0x01, 0x28, 0x03, 0x52, 0x0e, 0x73, 0x61, 0x6d, 0x70,
	0x6c, 0x65, 0x49, 0x6e, 0x74, 0x65, 0x72, 0x76, 0x61, 0x6c, 0x12, 0x23, 0x0a, 0x0d, 0x71, 0x75,
	0x65, 0x72, 0x79, 0x5f, 0x74, 0x69, 0x6d, 0x65, 0x6f, 0x75, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x03, 0x52, 0x0c, 0x71, 0x75, 0x65, 0x72, 0x79, 0x54, 0x69, 0x6d, 0x65, 0x6f, 0x75, 0x74, 0x12,
	0x2b, 0x0a, 0x11, 0x65, 0x78, 0x65, 0x63, 0x75, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x74, 0x68, 0x72,
	0x65, 0x61, 0x64, 0x73, 0x18, 0x03, 0x20, 0x01, 0x28, 0x03, 0x52, 0x10, 0x65, 0x78, 0x65, 0x63,
	0x75, 0x74, 0x69, 0x6f, 0x6e, 0x54, 0x68, 0x72, 0x65, 0x61, 0x64, 0x73, 0x12, 0x58, 0x0a, 0x10,
	0x63, 0x6c, 0x6f, 0x75, 0x64, 0x5f, 0x70, 0x72, 0x6f, 0x70, 0x65, 0x72, 0x74, 0x69, 0x65, 0x73,
	0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x2d, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e,
	0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x69, 0x6e, 0x73, 0x74, 0x61, 0x6e, 0x63,
	0x65, 0x69, 0x6e, 0x66, 0x6f, 0x2e, 0x43, 0x6c, 0x6f, 0x75, 0x64, 0x50, 0x72, 0x6f, 0x70, 0x65,
	0x72, 0x74, 0x69, 0x65, 0x73, 0x52, 0x0f, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x50, 0x72, 0x6f, 0x70,
	0x65, 0x72, 0x74, 0x69, 0x65, 0x73, 0x12, 0x54, 0x0a, 0x0b, 0x67, 0x63, 0x6c, 0x6f, 0x75, 0x64,
	0x5f, 0x61, 0x75, 0x74, 0x68, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x33, 0x2e, 0x73, 0x61,
	0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x68, 0x61,
	0x6e, 0x61, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d, 0x69, 0x67, 0x72,
	0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x47, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x41, 0x75, 0x74, 0x68,
	0x52, 0x0a, 0x67, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x41, 0x75, 0x74, 0x68, 0x12, 0x5c, 0x0a, 0x0e,
	0x68, 0x61, 0x6e, 0x61, 0x5f, 0x69, 0x6e, 0x73, 0x74, 0x61, 0x6e, 0x63, 0x65, 0x73, 0x18, 0x06,
	0x20, 0x03, 0x28, 0x0b, 0x32, 0x35, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f, 0x6e, 0x69, 0x74,
	0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d, 0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x48,
	0x41, 0x4e, 0x41, 0x49, 0x6e, 0x73, 0x74, 0x61, 0x6e, 0x63, 0x65, 0x52, 0x0d, 0x68, 0x61, 0x6e,
	0x61, 0x49, 0x6e, 0x73, 0x74, 0x61, 0x6e, 0x63, 0x65, 0x73, 0x22, 0x3e, 0x0a, 0x0a, 0x47, 0x63,
	0x6c, 0x6f, 0x75, 0x64, 0x41, 0x75, 0x74, 0x68, 0x12, 0x30, 0x0a, 0x14, 0x73, 0x65, 0x72, 0x76,
	0x69, 0x63, 0x65, 0x5f, 0x61, 0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x5f, 0x6a, 0x73, 0x6f, 0x6e,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x12, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x41,
	0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x4a, 0x73, 0x6f, 0x6e, 0x22, 0xe2, 0x04, 0x0a, 0x0c, 0x48,
	0x41, 0x4e, 0x41, 0x49, 0x6e, 0x73, 0x74, 0x61, 0x6e, 0x63, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x6e,
	0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12,
	0x10, 0x0a, 0x03, 0x73, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x73, 0x69,
	0x64, 0x12, 0x12, 0x0a, 0x04, 0x68, 0x6f, 0x73, 0x74, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x04, 0x68, 0x6f, 0x73, 0x74, 0x12, 0x12, 0x0a, 0x04, 0x70, 0x6f, 0x72, 0x74, 0x18, 0x04, 0x20,
	0x01, 0x28, 0x03, 0x52, 0x04, 0x70, 0x6f, 0x72, 0x74, 0x12, 0x12, 0x0a, 0x04, 0x75, 0x73, 0x65,
	0x72, 0x18, 0x05, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x75, 0x73, 0x65, 0x72, 0x12, 0x1a, 0x0a,
	0x08, 0x70, 0x61, 0x73, 0x73, 0x77, 0x6f, 0x72, 0x64, 0x18, 0x06, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x08, 0x70, 0x61, 0x73, 0x73, 0x77, 0x6f, 0x72, 0x64, 0x12, 0x1f, 0x0a, 0x0b, 0x73, 0x65, 0x63,
	0x72, 0x65, 0x74, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x07, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0a,
	0x73, 0x65, 0x63, 0x72, 0x65, 0x74, 0x4e, 0x61, 0x6d, 0x65, 0x12, 0x1d, 0x0a, 0x0a, 0x65, 0x6e,
	0x61, 0x62, 0x6c, 0x65, 0x5f, 0x73, 0x73, 0x6c, 0x18, 0x08, 0x20, 0x01, 0x28, 0x08, 0x52, 0x09,
	0x65, 0x6e, 0x61, 0x62, 0x6c, 0x65, 0x53, 0x73, 0x6c, 0x12, 0x31, 0x0a, 0x14, 0x76, 0x61, 0x6c,
	0x69, 0x64, 0x61, 0x74, 0x65, 0x5f, 0x63, 0x65, 0x72, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74,
	0x65, 0x18, 0x09, 0x20, 0x01, 0x28, 0x08, 0x52, 0x13, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74,
	0x65, 0x43, 0x65, 0x72, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x65, 0x12, 0x37, 0x0a, 0x18,
	0x68, 0x6f, 0x73, 0x74, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x5f, 0x69, 0x6e, 0x5f, 0x63, 0x65, 0x72,
	0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x65, 0x18, 0x0a, 0x20, 0x01, 0x28, 0x09, 0x52, 0x15,
	0x68, 0x6f, 0x73, 0x74, 0x4e, 0x61, 0x6d, 0x65, 0x49, 0x6e, 0x43, 0x65, 0x72, 0x74, 0x69, 0x66,
	0x69, 0x63, 0x61, 0x74, 0x65, 0x12, 0x20, 0x0a, 0x0b, 0x63, 0x6f, 0x6e, 0x6e, 0x65, 0x63, 0x74,
	0x69, 0x6f, 0x6e, 0x73, 0x18, 0x0b, 0x20, 0x01, 0x28, 0x03, 0x52, 0x0b, 0x63, 0x6f, 0x6e, 0x6e,
	0x65, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x12, 0x1b, 0x0a, 0x09, 0x6b, 0x65, 0x79, 0x5f, 0x73,
	0x74, 0x6f, 0x72, 0x65, 0x18, 0x0c, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x6b, 0x65, 0x79, 0x53,
	0x74, 0x6f, 0x72, 0x65, 0x12, 0x2c, 0x0a, 0x12, 0x6b, 0x65, 0x79, 0x5f, 0x73, 0x74, 0x6f, 0x72,
	0x65, 0x5f, 0x70, 0x61, 0x73, 0x73, 0x77, 0x6f, 0x72, 0x64, 0x18, 0x0d, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x10, 0x6b, 0x65, 0x79, 0x53, 0x74, 0x6f, 0x72, 0x65, 0x50, 0x61, 0x73, 0x73, 0x77, 0x6f,
	0x72, 0x64, 0x12, 0x31, 0x0a, 0x15, 0x6b, 0x65, 0x79, 0x5f, 0x73, 0x74, 0x6f, 0x72, 0x65, 0x5f,
	0x73, 0x65, 0x63, 0x72, 0x65, 0x74, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x0e, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x12, 0x6b, 0x65, 0x79, 0x53, 0x74, 0x6f, 0x72, 0x65, 0x53, 0x65, 0x63, 0x72, 0x65,
	0x74, 0x4e, 0x61, 0x6d, 0x65, 0x12, 0x1f, 0x0a, 0x0b, 0x74, 0x72, 0x75, 0x73, 0x74, 0x5f, 0x73,
	0x74, 0x6f, 0x72, 0x65, 0x18, 0x0f, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0a, 0x74, 0x72, 0x75, 0x73,
	0x74, 0x53, 0x74, 0x6f, 0x72, 0x65, 0x12, 0x30, 0x0a, 0x14, 0x74, 0x72, 0x75, 0x73, 0x74, 0x5f,
	0x73, 0x74, 0x6f, 0x72, 0x65, 0x5f, 0x70, 0x61, 0x73, 0x73, 0x77, 0x6f, 0x72, 0x64, 0x18, 0x10,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x12, 0x74, 0x72, 0x75, 0x73, 0x74, 0x53, 0x74, 0x6f, 0x72, 0x65,
	0x50, 0x61, 0x73, 0x73, 0x77, 0x6f, 0x72, 0x64, 0x12, 0x35, 0x0a, 0x17, 0x74, 0x72, 0x75, 0x73,
	0x74, 0x5f, 0x73, 0x74, 0x6f, 0x72, 0x65, 0x5f, 0x73, 0x65, 0x63, 0x72, 0x65, 0x74, 0x5f, 0x6e,
	0x61, 0x6d, 0x65, 0x18, 0x11, 0x20, 0x01, 0x28, 0x09, 0x52, 0x14, 0x74, 0x72, 0x75, 0x73, 0x74,
	0x53, 0x74, 0x6f, 0x72, 0x65, 0x53, 0x65, 0x63, 0x72, 0x65, 0x74, 0x4e, 0x61, 0x6d, 0x65, 0x22,
	0xf7, 0x01, 0x0a, 0x05, 0x51, 0x75, 0x65, 0x72, 0x79, 0x12, 0x18, 0x0a, 0x07, 0x65, 0x6e, 0x61,
	0x62, 0x6c, 0x65, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x08, 0x52, 0x07, 0x65, 0x6e, 0x61, 0x62,
	0x6c, 0x65, 0x64, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12, 0x20, 0x0a, 0x0b, 0x64, 0x65, 0x73, 0x63, 0x72,
	0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0b, 0x64, 0x65,
	0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x10, 0x0a, 0x03, 0x73, 0x71, 0x6c,
	0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x73, 0x71, 0x6c, 0x12, 0x27, 0x0a, 0x0f, 0x73,
	0x61, 0x6d, 0x70, 0x6c, 0x65, 0x5f, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x76, 0x61, 0x6c, 0x18, 0x05,
	0x20, 0x01, 0x28, 0x03, 0x52, 0x0e, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x49, 0x6e, 0x74, 0x65,
	0x72, 0x76, 0x61, 0x6c, 0x12, 0x49, 0x0a, 0x07, 0x63, 0x6f, 0x6c, 0x75, 0x6d, 0x6e, 0x73, 0x18,
	0x06, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x2f, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f, 0x6e, 0x69,
	0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d, 0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e,
	0x43, 0x6f, 0x6c, 0x75, 0x6d, 0x6e, 0x52, 0x07, 0x63, 0x6f, 0x6c, 0x75, 0x6d, 0x6e, 0x73, 0x12,
	0x18, 0x0a, 0x07, 0x63, 0x75, 0x72, 0x61, 0x74, 0x65, 0x64, 0x18, 0x07, 0x20, 0x01, 0x28, 0x08,
	0x52, 0x07, 0x63, 0x75, 0x72, 0x61, 0x74, 0x65, 0x64, 0x22, 0xa2, 0x02, 0x0a, 0x06, 0x43, 0x6f,
	0x6c, 0x75, 0x6d, 0x6e, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12, 0x20, 0x0a, 0x0b, 0x64, 0x65, 0x73, 0x63,
	0x72, 0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0b, 0x64,
	0x65, 0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x14, 0x0a, 0x05, 0x75, 0x6e,
	0x69, 0x74, 0x73, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x75, 0x6e, 0x69, 0x74, 0x73,
	0x12, 0x54, 0x0a, 0x0b, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x18,
	0x04, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x33, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x68, 0x61, 0x6e, 0x61, 0x6d, 0x6f, 0x6e, 0x69,
	0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d, 0x69, 0x67, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e,
	0x4d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x54, 0x79, 0x70, 0x65, 0x52, 0x0a, 0x6d, 0x65, 0x74, 0x72,
	0x69, 0x63, 0x54, 0x79, 0x70, 0x65, 0x12, 0x51, 0x0a, 0x0a, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x5f,
	0x74, 0x79, 0x70, 0x65, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x32, 0x2e, 0x73, 0x61, 0x70,
	0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x68, 0x61, 0x6e,
	0x61, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x6d, 0x69, 0x67, 0x72, 0x61,
	0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x54, 0x79, 0x70, 0x65, 0x52, 0x09,
	0x76, 0x61, 0x6c, 0x75, 0x65, 0x54, 0x79, 0x70, 0x65, 0x12, 0x23, 0x0a, 0x0d, 0x6e, 0x61, 0x6d,
	0x65, 0x5f, 0x6f, 0x76, 0x65, 0x72, 0x72, 0x69, 0x64, 0x65, 0x18, 0x06, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x0c, 0x6e, 0x61, 0x6d, 0x65, 0x4f, 0x76, 0x65, 0x72, 0x72, 0x69, 0x64, 0x65, 0x2a, 0x55,
	0x0a, 0x0a, 0x4d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x54, 0x79, 0x70, 0x65, 0x12, 0x16, 0x0a, 0x12,
	0x4d, 0x45, 0x54, 0x52, 0x49, 0x43, 0x5f, 0x55, 0x4e, 0x53, 0x50, 0x45, 0x43, 0x49, 0x46, 0x49,
	0x45, 0x44, 0x10, 0x00, 0x12, 0x09, 0x0a, 0x05, 0x4c, 0x41, 0x42, 0x45, 0x4c, 0x10, 0x01, 0x12,
	0x09, 0x0a, 0x05, 0x47, 0x41, 0x55, 0x47, 0x45, 0x10, 0x02, 0x12, 0x0e, 0x0a, 0x0a, 0x43, 0x55,
	0x4d, 0x55, 0x4c, 0x41, 0x54, 0x49, 0x56, 0x45, 0x10, 0x03, 0x12, 0x09, 0x0a, 0x05, 0x44, 0x45,
	0x4c, 0x54, 0x41, 0x10, 0x04, 0x2a, 0x61, 0x0a, 0x09, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x54, 0x79,
	0x70, 0x65, 0x12, 0x15, 0x0a, 0x11, 0x56, 0x41, 0x4c, 0x55, 0x45, 0x5f, 0x55, 0x4e, 0x53, 0x50,
	0x45, 0x43, 0x49, 0x46, 0x49, 0x45, 0x44, 0x10, 0x00, 0x12, 0x08, 0x0a, 0x04, 0x42, 0x4f, 0x4f,
	0x4c, 0x10, 0x01, 0x12, 0x09, 0x0a, 0x05, 0x49, 0x4e, 0x54, 0x36, 0x34, 0x10, 0x02, 0x12, 0x0a,
	0x0a, 0x06, 0x53, 0x54, 0x52, 0x49, 0x4e, 0x47, 0x10, 0x03, 0x12, 0x0a, 0x0a, 0x06, 0x44, 0x4f,
	0x55, 0x42, 0x4c, 0x45, 0x10, 0x04, 0x12, 0x10, 0x0a, 0x0c, 0x44, 0x49, 0x53, 0x54, 0x52, 0x49,
	0x42, 0x55, 0x54, 0x49, 0x4f, 0x4e, 0x10, 0x05, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescOnce sync.Once
	file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescData = file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDesc
)

func file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescGZIP() []byte {
	file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescOnce.Do(func() {
		file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescData = protoimpl.X.CompressGZIP(file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescData)
	})
	return file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDescData
}

var file_hanamonitoringmigration_hanamonitoringmigration_proto_enumTypes = make([]protoimpl.EnumInfo, 2)
var file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes = make([]protoimpl.MessageInfo, 6)
var file_hanamonitoringmigration_hanamonitoringmigration_proto_goTypes = []interface{}{
	(MetricType)(0),                      // 0: sapagent.protos.hanamonitoringmigration.MetricType
	(ValueType)(0),                       // 1: sapagent.protos.hanamonitoringmigration.ValueType
	(*HANAMonitoringConfiguration)(nil),  // 2: sapagent.protos.hanamonitoringmigration.HANAMonitoringConfiguration
	(*AgentConfig)(nil),                  // 3: sapagent.protos.hanamonitoringmigration.AgentConfig
	(*GcloudAuth)(nil),                   // 4: sapagent.protos.hanamonitoringmigration.GcloudAuth
	(*HANAInstance)(nil),                 // 5: sapagent.protos.hanamonitoringmigration.HANAInstance
	(*Query)(nil),                        // 6: sapagent.protos.hanamonitoringmigration.Query
	(*Column)(nil),                       // 7: sapagent.protos.hanamonitoringmigration.Column
	(*instanceinfo.CloudProperties)(nil), // 8: sapagent.protos.instanceinfo.CloudProperties
}
var file_hanamonitoringmigration_hanamonitoringmigration_proto_depIdxs = []int32{
	3, // 0: sapagent.protos.hanamonitoringmigration.HANAMonitoringConfiguration.agent:type_name -> sapagent.protos.hanamonitoringmigration.AgentConfig
	6, // 1: sapagent.protos.hanamonitoringmigration.HANAMonitoringConfiguration.queries:type_name -> sapagent.protos.hanamonitoringmigration.Query
	8, // 2: sapagent.protos.hanamonitoringmigration.AgentConfig.cloud_properties:type_name -> sapagent.protos.instanceinfo.CloudProperties
	4, // 3: sapagent.protos.hanamonitoringmigration.AgentConfig.gcloud_auth:type_name -> sapagent.protos.hanamonitoringmigration.GcloudAuth
	5, // 4: sapagent.protos.hanamonitoringmigration.AgentConfig.hana_instances:type_name -> sapagent.protos.hanamonitoringmigration.HANAInstance
	7, // 5: sapagent.protos.hanamonitoringmigration.Query.columns:type_name -> sapagent.protos.hanamonitoringmigration.Column
	0, // 6: sapagent.protos.hanamonitoringmigration.Column.metric_type:type_name -> sapagent.protos.hanamonitoringmigration.MetricType
	1, // 7: sapagent.protos.hanamonitoringmigration.Column.value_type:type_name -> sapagent.protos.hanamonitoringmigration.ValueType
	8, // [8:8] is the sub-list for method output_type
	8, // [8:8] is the sub-list for method input_type
	8, // [8:8] is the sub-list for extension type_name
	8, // [8:8] is the sub-list for extension extendee
	0, // [0:8] is the sub-list for field type_name
}

func init() { file_hanamonitoringmigration_hanamonitoringmigration_proto_init() }
func file_hanamonitoringmigration_hanamonitoringmigration_proto_init() {
	if File_hanamonitoringmigration_hanamonitoringmigration_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*HANAMonitoringConfiguration); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*AgentConfig); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GcloudAuth); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*HANAInstance); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Query); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes[5].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Column); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDesc,
			NumEnums:      2,
			NumMessages:   6,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_hanamonitoringmigration_hanamonitoringmigration_proto_goTypes,
		DependencyIndexes: file_hanamonitoringmigration_hanamonitoringmigration_proto_depIdxs,
		EnumInfos:         file_hanamonitoringmigration_hanamonitoringmigration_proto_enumTypes,
		MessageInfos:      file_hanamonitoringmigration_hanamonitoringmigration_proto_msgTypes,
	}.Build()
	File_hanamonitoringmigration_hanamonitoringmigration_proto = out.File
	file_hanamonitoringmigration_hanamonitoringmigration_proto_rawDesc = nil
	file_hanamonitoringmigration_hanamonitoringmigration_proto_goTypes = nil
	file_hanamonitoringmigration_hanamonitoringmigration_proto_depIdxs = nil
}
