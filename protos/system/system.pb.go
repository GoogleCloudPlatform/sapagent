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
// 	protoc-gen-go v1.28.1
// 	protoc        v3.6.1
// source: system/system.proto

package system

import (
	timestamp "github.com/golang/protobuf/ptypes/timestamp"
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

// Different types of resources attached to the SAP system.
type SapDiscovery_Resource_ResourceType int32

const (
	// Undefined resource type.
	SapDiscovery_Resource_RESOURCE_TYPE_UNSPECIFIED SapDiscovery_Resource_ResourceType = 0
	// This is a compute resource.
	SapDiscovery_Resource_COMPUTE SapDiscovery_Resource_ResourceType = 1
	// This a storage resource.
	SapDiscovery_Resource_STORAGE SapDiscovery_Resource_ResourceType = 2
	// This is a network resource.
	SapDiscovery_Resource_NETWORK SapDiscovery_Resource_ResourceType = 3
)

// Enum value maps for SapDiscovery_Resource_ResourceType.
var (
	SapDiscovery_Resource_ResourceType_name = map[int32]string{
		0: "RESOURCE_TYPE_UNSPECIFIED",
		1: "COMPUTE",
		2: "STORAGE",
		3: "NETWORK",
	}
	SapDiscovery_Resource_ResourceType_value = map[string]int32{
		"RESOURCE_TYPE_UNSPECIFIED": 0,
		"COMPUTE":                   1,
		"STORAGE":                   2,
		"NETWORK":                   3,
	}
)

func (x SapDiscovery_Resource_ResourceType) Enum() *SapDiscovery_Resource_ResourceType {
	p := new(SapDiscovery_Resource_ResourceType)
	*p = x
	return p
}

func (x SapDiscovery_Resource_ResourceType) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SapDiscovery_Resource_ResourceType) Descriptor() protoreflect.EnumDescriptor {
	return file_system_system_proto_enumTypes[0].Descriptor()
}

func (SapDiscovery_Resource_ResourceType) Type() protoreflect.EnumType {
	return &file_system_system_proto_enumTypes[0]
}

func (x SapDiscovery_Resource_ResourceType) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SapDiscovery_Resource_ResourceType.Descriptor instead.
func (SapDiscovery_Resource_ResourceType) EnumDescriptor() ([]byte, []int) {
	return file_system_system_proto_rawDescGZIP(), []int{0, 1, 0}
}

// Status of the resource in the current discovery phase.
type SapDiscovery_Resource_ResourceStatus int32

const (
	SapDiscovery_Resource_RESOURCE_STATUS_UNSPECIFIED SapDiscovery_Resource_ResourceStatus = 0
	SapDiscovery_Resource_ADDED                       SapDiscovery_Resource_ResourceStatus = 1
	SapDiscovery_Resource_UPDATED                     SapDiscovery_Resource_ResourceStatus = 2
	SapDiscovery_Resource_REMOVED                     SapDiscovery_Resource_ResourceStatus = 3
	SapDiscovery_Resource_REPLACED                    SapDiscovery_Resource_ResourceStatus = 4
	SapDiscovery_Resource_MISSING                     SapDiscovery_Resource_ResourceStatus = 5
)

// Enum value maps for SapDiscovery_Resource_ResourceStatus.
var (
	SapDiscovery_Resource_ResourceStatus_name = map[int32]string{
		0: "RESOURCE_STATUS_UNSPECIFIED",
		1: "ADDED",
		2: "UPDATED",
		3: "REMOVED",
		4: "REPLACED",
		5: "MISSING",
	}
	SapDiscovery_Resource_ResourceStatus_value = map[string]int32{
		"RESOURCE_STATUS_UNSPECIFIED": 0,
		"ADDED":                       1,
		"UPDATED":                     2,
		"REMOVED":                     3,
		"REPLACED":                    4,
		"MISSING":                     5,
	}
)

func (x SapDiscovery_Resource_ResourceStatus) Enum() *SapDiscovery_Resource_ResourceStatus {
	p := new(SapDiscovery_Resource_ResourceStatus)
	*p = x
	return p
}

func (x SapDiscovery_Resource_ResourceStatus) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SapDiscovery_Resource_ResourceStatus) Descriptor() protoreflect.EnumDescriptor {
	return file_system_system_proto_enumTypes[1].Descriptor()
}

func (SapDiscovery_Resource_ResourceStatus) Type() protoreflect.EnumType {
	return &file_system_system_proto_enumTypes[1]
}

func (x SapDiscovery_Resource_ResourceStatus) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SapDiscovery_Resource_ResourceStatus.Descriptor instead.
func (SapDiscovery_Resource_ResourceStatus) EnumDescriptor() ([]byte, []int) {
	return file_system_system_proto_rawDescGZIP(), []int{0, 1, 1}
}

// The schema of SAP system discovery data.
type SapDiscovery struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The metadata for SAP system discovery data.
	Metadata *SapDiscovery_Metadata `protobuf:"bytes,1,opt,name=metadata,proto3" json:"metadata,omitempty"`
	// An SAP System must have a database.
	DatabaseLayer *SapDiscovery_Component `protobuf:"bytes,2,opt,name=database_layer,json=databaseLayer,proto3" json:"database_layer,omitempty"`
	// An SAP system may run without an application layer.
	ApplicationLayer *SapDiscovery_Component `protobuf:"bytes,3,opt,name=application_layer,json=applicationLayer,proto3" json:"application_layer,omitempty"`
	// A combination of database SID, database instance URI and tenant DB name
	// to make a unique identifier per-system.
	SystemId string `protobuf:"bytes,4,opt,name=system_id,json=systemId,proto3" json:"system_id,omitempty"`
	// Unix timestamp this system has been updated last.
	UpdateTime *timestamp.Timestamp `protobuf:"bytes,5,opt,name=update_time,json=updateTime,proto3" json:"update_time,omitempty"`
}

func (x *SapDiscovery) Reset() {
	*x = SapDiscovery{}
	if protoimpl.UnsafeEnabled {
		mi := &file_system_system_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SapDiscovery) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SapDiscovery) ProtoMessage() {}

func (x *SapDiscovery) ProtoReflect() protoreflect.Message {
	mi := &file_system_system_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SapDiscovery.ProtoReflect.Descriptor instead.
func (*SapDiscovery) Descriptor() ([]byte, []int) {
	return file_system_system_proto_rawDescGZIP(), []int{0}
}

func (x *SapDiscovery) GetMetadata() *SapDiscovery_Metadata {
	if x != nil {
		return x.Metadata
	}
	return nil
}

func (x *SapDiscovery) GetDatabaseLayer() *SapDiscovery_Component {
	if x != nil {
		return x.DatabaseLayer
	}
	return nil
}

func (x *SapDiscovery) GetApplicationLayer() *SapDiscovery_Component {
	if x != nil {
		return x.ApplicationLayer
	}
	return nil
}

func (x *SapDiscovery) GetSystemId() string {
	if x != nil {
		return x.SystemId
	}
	return ""
}

func (x *SapDiscovery) GetUpdateTime() *timestamp.Timestamp {
	if x != nil {
		return x.UpdateTime
	}
	return nil
}

// Message describing SAP discovery system metadata
type SapDiscovery_Metadata struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Customer defined, something like "E-commerce pre prod"
	DefinedSystem string `protobuf:"bytes,1,opt,name=defined_system,json=definedSystem,proto3" json:"defined_system,omitempty"`
	// This sap product name
	SapProduct string `protobuf:"bytes,2,opt,name=sap_product,json=sapProduct,proto3" json:"sap_product,omitempty"`
	// Should be "prod", "QA", "dev", "staging", etc.
	EnvironmentType string `protobuf:"bytes,3,opt,name=environment_type,json=environmentType,proto3" json:"environment_type,omitempty"`
	// Customer region string for customer's use. Does not represent GCP region.
	CustomerRegion string `protobuf:"bytes,4,opt,name=customer_region,json=customerRegion,proto3" json:"customer_region,omitempty"`
}

func (x *SapDiscovery_Metadata) Reset() {
	*x = SapDiscovery_Metadata{}
	if protoimpl.UnsafeEnabled {
		mi := &file_system_system_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SapDiscovery_Metadata) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SapDiscovery_Metadata) ProtoMessage() {}

func (x *SapDiscovery_Metadata) ProtoReflect() protoreflect.Message {
	mi := &file_system_system_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SapDiscovery_Metadata.ProtoReflect.Descriptor instead.
func (*SapDiscovery_Metadata) Descriptor() ([]byte, []int) {
	return file_system_system_proto_rawDescGZIP(), []int{0, 0}
}

func (x *SapDiscovery_Metadata) GetDefinedSystem() string {
	if x != nil {
		return x.DefinedSystem
	}
	return ""
}

func (x *SapDiscovery_Metadata) GetSapProduct() string {
	if x != nil {
		return x.SapProduct
	}
	return ""
}

func (x *SapDiscovery_Metadata) GetEnvironmentType() string {
	if x != nil {
		return x.EnvironmentType
	}
	return ""
}

func (x *SapDiscovery_Metadata) GetCustomerRegion() string {
	if x != nil {
		return x.CustomerRegion
	}
	return ""
}

// Message describing a resource.
type SapDiscovery_Resource struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The type of this resource.
	ResourceType SapDiscovery_Resource_ResourceType `protobuf:"varint,1,opt,name=resource_type,json=resourceType,proto3,enum=cloud.partners.sap.system.SapDiscovery_Resource_ResourceType" json:"resource_type,omitempty"`
	// ComputeInstance, ComputeDisk, VPC, Bare Metal server, etc.
	ResourceKind string `protobuf:"bytes,2,opt,name=resource_kind,json=resourceKind,proto3" json:"resource_kind,omitempty"`
	// URI of the resource, includes project, location, and name.
	ResourceUri string `protobuf:"bytes,3,opt,name=resource_uri,json=resourceUri,proto3" json:"resource_uri,omitempty"`
	// A list of resource URIs related to this resource.
	RelatedResources []string `protobuf:"bytes,4,rep,name=related_resources,json=relatedResources,proto3" json:"related_resources,omitempty"`
	// Unix timestamp of when this resource last had its discovery data updated.
	UpdateTime *timestamp.Timestamp `protobuf:"bytes,5,opt,name=update_time,json=updateTime,proto3" json:"update_time,omitempty"`
	// Indicates whether this is a new, updated, or missing resource.
	ResourceStatus SapDiscovery_Resource_ResourceStatus `protobuf:"varint,6,opt,name=resource_status,json=resourceStatus,proto3,enum=cloud.partners.sap.system.SapDiscovery_Resource_ResourceStatus" json:"resource_status,omitempty"`
}

func (x *SapDiscovery_Resource) Reset() {
	*x = SapDiscovery_Resource{}
	if protoimpl.UnsafeEnabled {
		mi := &file_system_system_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SapDiscovery_Resource) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SapDiscovery_Resource) ProtoMessage() {}

func (x *SapDiscovery_Resource) ProtoReflect() protoreflect.Message {
	mi := &file_system_system_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SapDiscovery_Resource.ProtoReflect.Descriptor instead.
func (*SapDiscovery_Resource) Descriptor() ([]byte, []int) {
	return file_system_system_proto_rawDescGZIP(), []int{0, 1}
}

func (x *SapDiscovery_Resource) GetResourceType() SapDiscovery_Resource_ResourceType {
	if x != nil {
		return x.ResourceType
	}
	return SapDiscovery_Resource_RESOURCE_TYPE_UNSPECIFIED
}

func (x *SapDiscovery_Resource) GetResourceKind() string {
	if x != nil {
		return x.ResourceKind
	}
	return ""
}

func (x *SapDiscovery_Resource) GetResourceUri() string {
	if x != nil {
		return x.ResourceUri
	}
	return ""
}

func (x *SapDiscovery_Resource) GetRelatedResources() []string {
	if x != nil {
		return x.RelatedResources
	}
	return nil
}

func (x *SapDiscovery_Resource) GetUpdateTime() *timestamp.Timestamp {
	if x != nil {
		return x.UpdateTime
	}
	return nil
}

func (x *SapDiscovery_Resource) GetResourceStatus() SapDiscovery_Resource_ResourceStatus {
	if x != nil {
		return x.ResourceStatus
	}
	return SapDiscovery_Resource_RESOURCE_STATUS_UNSPECIFIED
}

// Message describing the system component.
type SapDiscovery_Component struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The resources in a component.
	Resources []*SapDiscovery_Resource `protobuf:"bytes,1,rep,name=resources,proto3" json:"resources,omitempty"`
	// Types that are assignable to Type:
	//
	//	*SapDiscovery_Component_ApplicationType
	//	*SapDiscovery_Component_DatabaseType
	Type isSapDiscovery_Component_Type `protobuf_oneof:"type"`
	// Pantheon Project in which the resources reside.
	HostProject string `protobuf:"bytes,4,opt,name=host_project,json=hostProject,proto3" json:"host_project,omitempty"`
	// The sap identifier,
	// used by the SAP software and helps differentiate systems for customers.
	Sid string `protobuf:"bytes,5,opt,name=sid,proto3" json:"sid,omitempty"`
}

func (x *SapDiscovery_Component) Reset() {
	*x = SapDiscovery_Component{}
	if protoimpl.UnsafeEnabled {
		mi := &file_system_system_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SapDiscovery_Component) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SapDiscovery_Component) ProtoMessage() {}

func (x *SapDiscovery_Component) ProtoReflect() protoreflect.Message {
	mi := &file_system_system_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SapDiscovery_Component.ProtoReflect.Descriptor instead.
func (*SapDiscovery_Component) Descriptor() ([]byte, []int) {
	return file_system_system_proto_rawDescGZIP(), []int{0, 2}
}

func (x *SapDiscovery_Component) GetResources() []*SapDiscovery_Resource {
	if x != nil {
		return x.Resources
	}
	return nil
}

func (m *SapDiscovery_Component) GetType() isSapDiscovery_Component_Type {
	if m != nil {
		return m.Type
	}
	return nil
}

func (x *SapDiscovery_Component) GetApplicationType() string {
	if x, ok := x.GetType().(*SapDiscovery_Component_ApplicationType); ok {
		return x.ApplicationType
	}
	return ""
}

func (x *SapDiscovery_Component) GetDatabaseType() string {
	if x, ok := x.GetType().(*SapDiscovery_Component_DatabaseType); ok {
		return x.DatabaseType
	}
	return ""
}

func (x *SapDiscovery_Component) GetHostProject() string {
	if x != nil {
		return x.HostProject
	}
	return ""
}

func (x *SapDiscovery_Component) GetSid() string {
	if x != nil {
		return x.Sid
	}
	return ""
}

type isSapDiscovery_Component_Type interface {
	isSapDiscovery_Component_Type()
}

type SapDiscovery_Component_ApplicationType struct {
	// The component is a SAP application.
	ApplicationType string `protobuf:"bytes,2,opt,name=application_type,json=applicationType,proto3,oneof"`
}

type SapDiscovery_Component_DatabaseType struct {
	// The component is a SAP database.
	DatabaseType string `protobuf:"bytes,3,opt,name=database_type,json=databaseType,proto3,oneof"`
}

func (*SapDiscovery_Component_ApplicationType) isSapDiscovery_Component_Type() {}

func (*SapDiscovery_Component_DatabaseType) isSapDiscovery_Component_Type() {}

var File_system_system_proto protoreflect.FileDescriptor

var file_system_system_proto_rawDesc = []byte{
	0x0a, 0x13, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x2f, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x19, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x2e, 0x70, 0x61, 0x72,
	0x74, 0x6e, 0x65, 0x72, 0x73, 0x2e, 0x73, 0x61, 0x70, 0x2e, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d,
	0x1a, 0x1f, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75,
	0x66, 0x2f, 0x74, 0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x22, 0xde, 0x0a, 0x0a, 0x0c, 0x53, 0x61, 0x70, 0x44, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65,
	0x72, 0x79, 0x12, 0x4c, 0x0a, 0x08, 0x6d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x18, 0x01,
	0x20, 0x01, 0x28, 0x0b, 0x32, 0x30, 0x2e, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x2e, 0x70, 0x61, 0x72,
	0x74, 0x6e, 0x65, 0x72, 0x73, 0x2e, 0x73, 0x61, 0x70, 0x2e, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d,
	0x2e, 0x53, 0x61, 0x70, 0x44, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65, 0x72, 0x79, 0x2e, 0x4d, 0x65,
	0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x52, 0x08, 0x6d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61,
	0x12, 0x58, 0x0a, 0x0e, 0x64, 0x61, 0x74, 0x61, 0x62, 0x61, 0x73, 0x65, 0x5f, 0x6c, 0x61, 0x79,
	0x65, 0x72, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x31, 0x2e, 0x63, 0x6c, 0x6f, 0x75, 0x64,
	0x2e, 0x70, 0x61, 0x72, 0x74, 0x6e, 0x65, 0x72, 0x73, 0x2e, 0x73, 0x61, 0x70, 0x2e, 0x73, 0x79,
	0x73, 0x74, 0x65, 0x6d, 0x2e, 0x53, 0x61, 0x70, 0x44, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65, 0x72,
	0x79, 0x2e, 0x43, 0x6f, 0x6d, 0x70, 0x6f, 0x6e, 0x65, 0x6e, 0x74, 0x52, 0x0d, 0x64, 0x61, 0x74,
	0x61, 0x62, 0x61, 0x73, 0x65, 0x4c, 0x61, 0x79, 0x65, 0x72, 0x12, 0x5e, 0x0a, 0x11, 0x61, 0x70,
	0x70, 0x6c, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x6c, 0x61, 0x79, 0x65, 0x72, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x31, 0x2e, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x2e, 0x70, 0x61,
	0x72, 0x74, 0x6e, 0x65, 0x72, 0x73, 0x2e, 0x73, 0x61, 0x70, 0x2e, 0x73, 0x79, 0x73, 0x74, 0x65,
	0x6d, 0x2e, 0x53, 0x61, 0x70, 0x44, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65, 0x72, 0x79, 0x2e, 0x43,
	0x6f, 0x6d, 0x70, 0x6f, 0x6e, 0x65, 0x6e, 0x74, 0x52, 0x10, 0x61, 0x70, 0x70, 0x6c, 0x69, 0x63,
	0x61, 0x74, 0x69, 0x6f, 0x6e, 0x4c, 0x61, 0x79, 0x65, 0x72, 0x12, 0x1b, 0x0a, 0x09, 0x73, 0x79,
	0x73, 0x74, 0x65, 0x6d, 0x5f, 0x69, 0x64, 0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x73,
	0x79, 0x73, 0x74, 0x65, 0x6d, 0x49, 0x64, 0x12, 0x3b, 0x0a, 0x0b, 0x75, 0x70, 0x64, 0x61, 0x74,
	0x65, 0x5f, 0x74, 0x69, 0x6d, 0x65, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67,
	0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54,
	0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x52, 0x0a, 0x75, 0x70, 0x64, 0x61, 0x74, 0x65,
	0x54, 0x69, 0x6d, 0x65, 0x1a, 0xa6, 0x01, 0x0a, 0x08, 0x4d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74,
	0x61, 0x12, 0x25, 0x0a, 0x0e, 0x64, 0x65, 0x66, 0x69, 0x6e, 0x65, 0x64, 0x5f, 0x73, 0x79, 0x73,
	0x74, 0x65, 0x6d, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0d, 0x64, 0x65, 0x66, 0x69, 0x6e,
	0x65, 0x64, 0x53, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x12, 0x1f, 0x0a, 0x0b, 0x73, 0x61, 0x70, 0x5f,
	0x70, 0x72, 0x6f, 0x64, 0x75, 0x63, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0a, 0x73,
	0x61, 0x70, 0x50, 0x72, 0x6f, 0x64, 0x75, 0x63, 0x74, 0x12, 0x29, 0x0a, 0x10, 0x65, 0x6e, 0x76,
	0x69, 0x72, 0x6f, 0x6e, 0x6d, 0x65, 0x6e, 0x74, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x18, 0x03, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x0f, 0x65, 0x6e, 0x76, 0x69, 0x72, 0x6f, 0x6e, 0x6d, 0x65, 0x6e, 0x74,
	0x54, 0x79, 0x70, 0x65, 0x12, 0x27, 0x0a, 0x0f, 0x63, 0x75, 0x73, 0x74, 0x6f, 0x6d, 0x65, 0x72,
	0x5f, 0x72, 0x65, 0x67, 0x69, 0x6f, 0x6e, 0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0e, 0x63,
	0x75, 0x73, 0x74, 0x6f, 0x6d, 0x65, 0x72, 0x52, 0x65, 0x67, 0x69, 0x6f, 0x6e, 0x1a, 0xd3, 0x04,
	0x0a, 0x08, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x12, 0x62, 0x0a, 0x0d, 0x72, 0x65,
	0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x0e, 0x32, 0x3d, 0x2e, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x2e, 0x70, 0x61, 0x72, 0x74, 0x6e, 0x65,
	0x72, 0x73, 0x2e, 0x73, 0x61, 0x70, 0x2e, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x2e, 0x53, 0x61,
	0x70, 0x44, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65, 0x72, 0x79, 0x2e, 0x52, 0x65, 0x73, 0x6f, 0x75,
	0x72, 0x63, 0x65, 0x2e, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x54, 0x79, 0x70, 0x65,
	0x52, 0x0c, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x54, 0x79, 0x70, 0x65, 0x12, 0x23,
	0x0a, 0x0d, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x6b, 0x69, 0x6e, 0x64, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0c, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x4b,
	0x69, 0x6e, 0x64, 0x12, 0x21, 0x0a, 0x0c, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f,
	0x75, 0x72, 0x69, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0b, 0x72, 0x65, 0x73, 0x6f, 0x75,
	0x72, 0x63, 0x65, 0x55, 0x72, 0x69, 0x12, 0x2b, 0x0a, 0x11, 0x72, 0x65, 0x6c, 0x61, 0x74, 0x65,
	0x64, 0x5f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x73, 0x18, 0x04, 0x20, 0x03, 0x28,
	0x09, 0x52, 0x10, 0x72, 0x65, 0x6c, 0x61, 0x74, 0x65, 0x64, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72,
	0x63, 0x65, 0x73, 0x12, 0x3b, 0x0a, 0x0b, 0x75, 0x70, 0x64, 0x61, 0x74, 0x65, 0x5f, 0x74, 0x69,
	0x6d, 0x65, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c,
	0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69, 0x6d, 0x65, 0x73,
	0x74, 0x61, 0x6d, 0x70, 0x52, 0x0a, 0x75, 0x70, 0x64, 0x61, 0x74, 0x65, 0x54, 0x69, 0x6d, 0x65,
	0x12, 0x68, 0x0a, 0x0f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x73, 0x74, 0x61,
	0x74, 0x75, 0x73, 0x18, 0x06, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x3f, 0x2e, 0x63, 0x6c, 0x6f, 0x75,
	0x64, 0x2e, 0x70, 0x61, 0x72, 0x74, 0x6e, 0x65, 0x72, 0x73, 0x2e, 0x73, 0x61, 0x70, 0x2e, 0x73,
	0x79, 0x73, 0x74, 0x65, 0x6d, 0x2e, 0x53, 0x61, 0x70, 0x44, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65,
	0x72, 0x79, 0x2e, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x2e, 0x52, 0x65, 0x73, 0x6f,
	0x75, 0x72, 0x63, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x52, 0x0e, 0x72, 0x65, 0x73, 0x6f,
	0x75, 0x72, 0x63, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x22, 0x54, 0x0a, 0x0c, 0x52, 0x65,
	0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x54, 0x79, 0x70, 0x65, 0x12, 0x1d, 0x0a, 0x19, 0x52, 0x45,
	0x53, 0x4f, 0x55, 0x52, 0x43, 0x45, 0x5f, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x55, 0x4e, 0x53, 0x50,
	0x45, 0x43, 0x49, 0x46, 0x49, 0x45, 0x44, 0x10, 0x00, 0x12, 0x0b, 0x0a, 0x07, 0x43, 0x4f, 0x4d,
	0x50, 0x55, 0x54, 0x45, 0x10, 0x01, 0x12, 0x0b, 0x0a, 0x07, 0x53, 0x54, 0x4f, 0x52, 0x41, 0x47,
	0x45, 0x10, 0x02, 0x12, 0x0b, 0x0a, 0x07, 0x4e, 0x45, 0x54, 0x57, 0x4f, 0x52, 0x4b, 0x10, 0x03,
	0x22, 0x71, 0x0a, 0x0e, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x53, 0x74, 0x61, 0x74,
	0x75, 0x73, 0x12, 0x1f, 0x0a, 0x1b, 0x52, 0x45, 0x53, 0x4f, 0x55, 0x52, 0x43, 0x45, 0x5f, 0x53,
	0x54, 0x41, 0x54, 0x55, 0x53, 0x5f, 0x55, 0x4e, 0x53, 0x50, 0x45, 0x43, 0x49, 0x46, 0x49, 0x45,
	0x44, 0x10, 0x00, 0x12, 0x09, 0x0a, 0x05, 0x41, 0x44, 0x44, 0x45, 0x44, 0x10, 0x01, 0x12, 0x0b,
	0x0a, 0x07, 0x55, 0x50, 0x44, 0x41, 0x54, 0x45, 0x44, 0x10, 0x02, 0x12, 0x0b, 0x0a, 0x07, 0x52,
	0x45, 0x4d, 0x4f, 0x56, 0x45, 0x44, 0x10, 0x03, 0x12, 0x0c, 0x0a, 0x08, 0x52, 0x45, 0x50, 0x4c,
	0x41, 0x43, 0x45, 0x44, 0x10, 0x04, 0x12, 0x0b, 0x0a, 0x07, 0x4d, 0x49, 0x53, 0x53, 0x49, 0x4e,
	0x47, 0x10, 0x05, 0x1a, 0xec, 0x01, 0x0a, 0x09, 0x43, 0x6f, 0x6d, 0x70, 0x6f, 0x6e, 0x65, 0x6e,
	0x74, 0x12, 0x4e, 0x0a, 0x09, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x73, 0x18, 0x01,
	0x20, 0x03, 0x28, 0x0b, 0x32, 0x30, 0x2e, 0x63, 0x6c, 0x6f, 0x75, 0x64, 0x2e, 0x70, 0x61, 0x72,
	0x74, 0x6e, 0x65, 0x72, 0x73, 0x2e, 0x73, 0x61, 0x70, 0x2e, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d,
	0x2e, 0x53, 0x61, 0x70, 0x44, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65, 0x72, 0x79, 0x2e, 0x52, 0x65,
	0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x52, 0x09, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65,
	0x73, 0x12, 0x2b, 0x0a, 0x10, 0x61, 0x70, 0x70, 0x6c, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e,
	0x5f, 0x74, 0x79, 0x70, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x48, 0x00, 0x52, 0x0f, 0x61,
	0x70, 0x70, 0x6c, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x54, 0x79, 0x70, 0x65, 0x12, 0x25,
	0x0a, 0x0d, 0x64, 0x61, 0x74, 0x61, 0x62, 0x61, 0x73, 0x65, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x09, 0x48, 0x00, 0x52, 0x0c, 0x64, 0x61, 0x74, 0x61, 0x62, 0x61, 0x73,
	0x65, 0x54, 0x79, 0x70, 0x65, 0x12, 0x21, 0x0a, 0x0c, 0x68, 0x6f, 0x73, 0x74, 0x5f, 0x70, 0x72,
	0x6f, 0x6a, 0x65, 0x63, 0x74, 0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0b, 0x68, 0x6f, 0x73,
	0x74, 0x50, 0x72, 0x6f, 0x6a, 0x65, 0x63, 0x74, 0x12, 0x10, 0x0a, 0x03, 0x73, 0x69, 0x64, 0x18,
	0x05, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x73, 0x69, 0x64, 0x42, 0x06, 0x0a, 0x04, 0x74, 0x79,
	0x70, 0x65, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_system_system_proto_rawDescOnce sync.Once
	file_system_system_proto_rawDescData = file_system_system_proto_rawDesc
)

func file_system_system_proto_rawDescGZIP() []byte {
	file_system_system_proto_rawDescOnce.Do(func() {
		file_system_system_proto_rawDescData = protoimpl.X.CompressGZIP(file_system_system_proto_rawDescData)
	})
	return file_system_system_proto_rawDescData
}

var file_system_system_proto_enumTypes = make([]protoimpl.EnumInfo, 2)
var file_system_system_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_system_system_proto_goTypes = []interface{}{
	(SapDiscovery_Resource_ResourceType)(0),   // 0: cloud.partners.sap.system.SapDiscovery.Resource.ResourceType
	(SapDiscovery_Resource_ResourceStatus)(0), // 1: cloud.partners.sap.system.SapDiscovery.Resource.ResourceStatus
	(*SapDiscovery)(nil),                      // 2: cloud.partners.sap.system.SapDiscovery
	(*SapDiscovery_Metadata)(nil),             // 3: cloud.partners.sap.system.SapDiscovery.Metadata
	(*SapDiscovery_Resource)(nil),             // 4: cloud.partners.sap.system.SapDiscovery.Resource
	(*SapDiscovery_Component)(nil),            // 5: cloud.partners.sap.system.SapDiscovery.Component
	(*timestamp.Timestamp)(nil),               // 6: google.protobuf.Timestamp
}
var file_system_system_proto_depIdxs = []int32{
	3, // 0: cloud.partners.sap.system.SapDiscovery.metadata:type_name -> cloud.partners.sap.system.SapDiscovery.Metadata
	5, // 1: cloud.partners.sap.system.SapDiscovery.database_layer:type_name -> cloud.partners.sap.system.SapDiscovery.Component
	5, // 2: cloud.partners.sap.system.SapDiscovery.application_layer:type_name -> cloud.partners.sap.system.SapDiscovery.Component
	6, // 3: cloud.partners.sap.system.SapDiscovery.update_time:type_name -> google.protobuf.Timestamp
	0, // 4: cloud.partners.sap.system.SapDiscovery.Resource.resource_type:type_name -> cloud.partners.sap.system.SapDiscovery.Resource.ResourceType
	6, // 5: cloud.partners.sap.system.SapDiscovery.Resource.update_time:type_name -> google.protobuf.Timestamp
	1, // 6: cloud.partners.sap.system.SapDiscovery.Resource.resource_status:type_name -> cloud.partners.sap.system.SapDiscovery.Resource.ResourceStatus
	4, // 7: cloud.partners.sap.system.SapDiscovery.Component.resources:type_name -> cloud.partners.sap.system.SapDiscovery.Resource
	8, // [8:8] is the sub-list for method output_type
	8, // [8:8] is the sub-list for method input_type
	8, // [8:8] is the sub-list for extension type_name
	8, // [8:8] is the sub-list for extension extendee
	0, // [0:8] is the sub-list for field type_name
}

func init() { file_system_system_proto_init() }
func file_system_system_proto_init() {
	if File_system_system_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_system_system_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SapDiscovery); i {
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
		file_system_system_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SapDiscovery_Metadata); i {
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
		file_system_system_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SapDiscovery_Resource); i {
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
		file_system_system_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SapDiscovery_Component); i {
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
	file_system_system_proto_msgTypes[3].OneofWrappers = []interface{}{
		(*SapDiscovery_Component_ApplicationType)(nil),
		(*SapDiscovery_Component_DatabaseType)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_system_system_proto_rawDesc,
			NumEnums:      2,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_system_system_proto_goTypes,
		DependencyIndexes: file_system_system_proto_depIdxs,
		EnumInfos:         file_system_system_proto_enumTypes,
		MessageInfos:      file_system_system_proto_msgTypes,
	}.Build()
	File_system_system_proto = out.File
	file_system_system_proto_rawDesc = nil
	file_system_system_proto_goTypes = nil
	file_system_system_proto_depIdxs = nil
}
