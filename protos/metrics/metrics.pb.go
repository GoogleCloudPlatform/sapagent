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
// 	protoc-gen-go v1.34.1
// 	protoc        v3.6.1
// source: metrics/metrics.proto

package metrics

import (
	descriptor "github.com/golang/protobuf/protoc-gen-go/descriptor"
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

type Context int32

const (
	Context_CONTEXT_UNDEFINED Context = 0
	Context_CONTEXT_VM        Context = 1
	Context_CONTEXT_HOST      Context = 2
)

// Enum value maps for Context.
var (
	Context_name = map[int32]string{
		0: "CONTEXT_UNDEFINED",
		1: "CONTEXT_VM",
		2: "CONTEXT_HOST",
	}
	Context_value = map[string]int32{
		"CONTEXT_UNDEFINED": 0,
		"CONTEXT_VM":        1,
		"CONTEXT_HOST":      2,
	}
)

func (x Context) Enum() *Context {
	p := new(Context)
	*p = x
	return p
}

func (x Context) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (Context) Descriptor() protoreflect.EnumDescriptor {
	return file_metrics_metrics_proto_enumTypes[0].Descriptor()
}

func (Context) Type() protoreflect.EnumType {
	return &file_metrics_metrics_proto_enumTypes[0]
}

func (x Context) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use Context.Descriptor instead.
func (Context) EnumDescriptor() ([]byte, []int) {
	return file_metrics_metrics_proto_rawDescGZIP(), []int{0}
}

type Category int32

const (
	Category_CATEGORY_UNDEFINED Category = 0
	Category_CATEGORY_CONFIG    Category = 1
	Category_CATEGORY_CPU       Category = 2
	Category_CATEGORY_MEMORY    Category = 3
	Category_CATEGORY_NETWORK   Category = 4
	Category_CATEGORY_DISK      Category = 5
)

// Enum value maps for Category.
var (
	Category_name = map[int32]string{
		0: "CATEGORY_UNDEFINED",
		1: "CATEGORY_CONFIG",
		2: "CATEGORY_CPU",
		3: "CATEGORY_MEMORY",
		4: "CATEGORY_NETWORK",
		5: "CATEGORY_DISK",
	}
	Category_value = map[string]int32{
		"CATEGORY_UNDEFINED": 0,
		"CATEGORY_CONFIG":    1,
		"CATEGORY_CPU":       2,
		"CATEGORY_MEMORY":    3,
		"CATEGORY_NETWORK":   4,
		"CATEGORY_DISK":      5,
	}
)

func (x Category) Enum() *Category {
	p := new(Category)
	*p = x
	return p
}

func (x Category) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (Category) Descriptor() protoreflect.EnumDescriptor {
	return file_metrics_metrics_proto_enumTypes[1].Descriptor()
}

func (Category) Type() protoreflect.EnumType {
	return &file_metrics_metrics_proto_enumTypes[1]
}

func (x Category) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use Category.Descriptor instead.
func (Category) EnumDescriptor() ([]byte, []int) {
	return file_metrics_metrics_proto_rawDescGZIP(), []int{1}
}

type Type int32

const (
	Type_TYPE_UNDEFINED Type = 0
	Type_TYPE_INT32     Type = 1
	Type_TYPE_INT64     Type = 2
	Type_TYPE_UINT32    Type = 3
	Type_TYPE_UINT64    Type = 4
	Type_TYPE_REAL32    Type = 5
	Type_TYPE_REAL64    Type = 6
	Type_TYPE_DOUBLE    Type = 7
	Type_TYPE_STRING    Type = 8
)

// Enum value maps for Type.
var (
	Type_name = map[int32]string{
		0: "TYPE_UNDEFINED",
		1: "TYPE_INT32",
		2: "TYPE_INT64",
		3: "TYPE_UINT32",
		4: "TYPE_UINT64",
		5: "TYPE_REAL32",
		6: "TYPE_REAL64",
		7: "TYPE_DOUBLE",
		8: "TYPE_STRING",
	}
	Type_value = map[string]int32{
		"TYPE_UNDEFINED": 0,
		"TYPE_INT32":     1,
		"TYPE_INT64":     2,
		"TYPE_UINT32":    3,
		"TYPE_UINT64":    4,
		"TYPE_REAL32":    5,
		"TYPE_REAL64":    6,
		"TYPE_DOUBLE":    7,
		"TYPE_STRING":    8,
	}
)

func (x Type) Enum() *Type {
	p := new(Type)
	*p = x
	return p
}

func (x Type) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (Type) Descriptor() protoreflect.EnumDescriptor {
	return file_metrics_metrics_proto_enumTypes[2].Descriptor()
}

func (Type) Type() protoreflect.EnumType {
	return &file_metrics_metrics_proto_enumTypes[2]
}

func (x Type) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use Type.Descriptor instead.
func (Type) EnumDescriptor() ([]byte, []int) {
	return file_metrics_metrics_proto_rawDescGZIP(), []int{2}
}

type Unit int32

const (
	Unit_UNIT_UNDEFINED       Unit = 0
	Unit_UNIT_NONE            Unit = 1
	Unit_UNIT_MHZ             Unit = 2
	Unit_UNIT_CU              Unit = 3
	Unit_UNIT_PERCENT         Unit = 4
	Unit_UNIT_MB              Unit = 5
	Unit_UNIT_KBPS            Unit = 6
	Unit_UNIT_MBPS            Unit = 7
	Unit_UNIT_MSEC            Unit = 8
	Unit_UNIT_SEC             Unit = 9
	Unit_UNIT_BPS             Unit = 10
	Unit_UNIT_OPS_PER_SEC     Unit = 11
	Unit_UNIT_PACKETS_PER_SEC Unit = 12
)

// Enum value maps for Unit.
var (
	Unit_name = map[int32]string{
		0:  "UNIT_UNDEFINED",
		1:  "UNIT_NONE",
		2:  "UNIT_MHZ",
		3:  "UNIT_CU",
		4:  "UNIT_PERCENT",
		5:  "UNIT_MB",
		6:  "UNIT_KBPS",
		7:  "UNIT_MBPS",
		8:  "UNIT_MSEC",
		9:  "UNIT_SEC",
		10: "UNIT_BPS",
		11: "UNIT_OPS_PER_SEC",
		12: "UNIT_PACKETS_PER_SEC",
	}
	Unit_value = map[string]int32{
		"UNIT_UNDEFINED":       0,
		"UNIT_NONE":            1,
		"UNIT_MHZ":             2,
		"UNIT_CU":              3,
		"UNIT_PERCENT":         4,
		"UNIT_MB":              5,
		"UNIT_KBPS":            6,
		"UNIT_MBPS":            7,
		"UNIT_MSEC":            8,
		"UNIT_SEC":             9,
		"UNIT_BPS":             10,
		"UNIT_OPS_PER_SEC":     11,
		"UNIT_PACKETS_PER_SEC": 12,
	}
)

func (x Unit) Enum() *Unit {
	p := new(Unit)
	*p = x
	return p
}

func (x Unit) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (Unit) Descriptor() protoreflect.EnumDescriptor {
	return file_metrics_metrics_proto_enumTypes[3].Descriptor()
}

func (Unit) Type() protoreflect.EnumType {
	return &file_metrics_metrics_proto_enumTypes[3]
}

func (x Unit) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use Unit.Descriptor instead.
func (Unit) EnumDescriptor() ([]byte, []int) {
	return file_metrics_metrics_proto_rawDescGZIP(), []int{3}
}

type RefreshInterval int32

const (
	RefreshInterval_REFRESHINTERVAL_UNDEFINED  RefreshInterval = 0
	RefreshInterval_REFRESHINTERVAL_RESTART    RefreshInterval = 1
	RefreshInterval_REFRESHINTERVAL_PER_MINUTE RefreshInterval = 2
)

// Enum value maps for RefreshInterval.
var (
	RefreshInterval_name = map[int32]string{
		0: "REFRESHINTERVAL_UNDEFINED",
		1: "REFRESHINTERVAL_RESTART",
		2: "REFRESHINTERVAL_PER_MINUTE",
	}
	RefreshInterval_value = map[string]int32{
		"REFRESHINTERVAL_UNDEFINED":  0,
		"REFRESHINTERVAL_RESTART":    1,
		"REFRESHINTERVAL_PER_MINUTE": 2,
	}
)

func (x RefreshInterval) Enum() *RefreshInterval {
	p := new(RefreshInterval)
	*p = x
	return p
}

func (x RefreshInterval) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (RefreshInterval) Descriptor() protoreflect.EnumDescriptor {
	return file_metrics_metrics_proto_enumTypes[4].Descriptor()
}

func (RefreshInterval) Type() protoreflect.EnumType {
	return &file_metrics_metrics_proto_enumTypes[4]
}

func (x RefreshInterval) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use RefreshInterval.Descriptor instead.
func (RefreshInterval) EnumDescriptor() ([]byte, []int) {
	return file_metrics_metrics_proto_rawDescGZIP(), []int{4}
}

type MetricsCollection struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Metrics []*Metric `protobuf:"bytes,1,rep,name=metrics,proto3" json:"metrics,omitempty"`
}

func (x *MetricsCollection) Reset() {
	*x = MetricsCollection{}
	if protoimpl.UnsafeEnabled {
		mi := &file_metrics_metrics_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MetricsCollection) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MetricsCollection) ProtoMessage() {}

func (x *MetricsCollection) ProtoReflect() protoreflect.Message {
	mi := &file_metrics_metrics_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MetricsCollection.ProtoReflect.Descriptor instead.
func (*MetricsCollection) Descriptor() ([]byte, []int) {
	return file_metrics_metrics_proto_rawDescGZIP(), []int{0}
}

func (x *MetricsCollection) GetMetrics() []*Metric {
	if x != nil {
		return x.Metrics
	}
	return nil
}

type Metric struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Category        Category        `protobuf:"varint,1,opt,name=category,proto3,enum=sapagent.protos.metrics.Category" json:"category,omitempty"`
	Type            Type            `protobuf:"varint,2,opt,name=type,proto3,enum=sapagent.protos.metrics.Type" json:"type,omitempty"`
	Name            string          `protobuf:"bytes,3,opt,name=name,proto3" json:"name,omitempty"`
	Value           string          `protobuf:"bytes,4,opt,name=value,proto3" json:"value,omitempty"`
	Unit            Unit            `protobuf:"varint,5,opt,name=unit,proto3,enum=sapagent.protos.metrics.Unit" json:"unit,omitempty"`
	Context         Context         `protobuf:"varint,6,opt,name=context,proto3,enum=sapagent.protos.metrics.Context" json:"context,omitempty"`
	LastRefresh     int64           `protobuf:"varint,7,opt,name=last_refresh,json=lastRefresh,proto3" json:"last_refresh,omitempty"`
	RefreshInterval RefreshInterval `protobuf:"varint,8,opt,name=refresh_interval,json=refreshInterval,proto3,enum=sapagent.protos.metrics.RefreshInterval" json:"refresh_interval,omitempty"`
	DeviceId        string          `protobuf:"bytes,9,opt,name=device_id,json=deviceId,proto3" json:"device_id,omitempty"`
}

func (x *Metric) Reset() {
	*x = Metric{}
	if protoimpl.UnsafeEnabled {
		mi := &file_metrics_metrics_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Metric) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Metric) ProtoMessage() {}

func (x *Metric) ProtoReflect() protoreflect.Message {
	mi := &file_metrics_metrics_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Metric.ProtoReflect.Descriptor instead.
func (*Metric) Descriptor() ([]byte, []int) {
	return file_metrics_metrics_proto_rawDescGZIP(), []int{1}
}

func (x *Metric) GetCategory() Category {
	if x != nil {
		return x.Category
	}
	return Category_CATEGORY_UNDEFINED
}

func (x *Metric) GetType() Type {
	if x != nil {
		return x.Type
	}
	return Type_TYPE_UNDEFINED
}

func (x *Metric) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *Metric) GetValue() string {
	if x != nil {
		return x.Value
	}
	return ""
}

func (x *Metric) GetUnit() Unit {
	if x != nil {
		return x.Unit
	}
	return Unit_UNIT_UNDEFINED
}

func (x *Metric) GetContext() Context {
	if x != nil {
		return x.Context
	}
	return Context_CONTEXT_UNDEFINED
}

func (x *Metric) GetLastRefresh() int64 {
	if x != nil {
		return x.LastRefresh
	}
	return 0
}

func (x *Metric) GetRefreshInterval() RefreshInterval {
	if x != nil {
		return x.RefreshInterval
	}
	return RefreshInterval_REFRESHINTERVAL_UNDEFINED
}

func (x *Metric) GetDeviceId() string {
	if x != nil {
		return x.DeviceId
	}
	return ""
}

var file_metrics_metrics_proto_extTypes = []protoimpl.ExtensionInfo{
	{
		ExtendedType:  (*descriptor.EnumValueOptions)(nil),
		ExtensionType: (*string)(nil),
		Field:         460533978,
		Name:          "sapagent.protos.metrics.xml_name",
		Tag:           "bytes,460533978,opt,name=xml_name",
		Filename:      "metrics/metrics.proto",
	},
}

// Extension fields to descriptor.EnumValueOptions.
var (
	// optional string xml_name = 460533978;
	E_XmlName = &file_metrics_metrics_proto_extTypes[0]
)

var File_metrics_metrics_proto protoreflect.FileDescriptor

var file_metrics_metrics_proto_rawDesc = []byte{
	0x0a, 0x15, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x73, 0x2f, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63,
	0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x17, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e,
	0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x73,
	0x1a, 0x20, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75,
	0x66, 0x2f, 0x64, 0x65, 0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x6f, 0x72, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x22, 0x4e, 0x0a, 0x11, 0x4d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x73, 0x43, 0x6f, 0x6c,
	0x6c, 0x65, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x39, 0x0a, 0x07, 0x6d, 0x65, 0x74, 0x72, 0x69,
	0x63, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x1f, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67,
	0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x6d, 0x65, 0x74, 0x72, 0x69,
	0x63, 0x73, 0x2e, 0x4d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x52, 0x07, 0x6d, 0x65, 0x74, 0x72, 0x69,
	0x63, 0x73, 0x22, 0xa8, 0x03, 0x0a, 0x06, 0x4d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x12, 0x3d, 0x0a,
	0x08, 0x63, 0x61, 0x74, 0x65, 0x67, 0x6f, 0x72, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0e, 0x32,
	0x21, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x73, 0x2e, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x73, 0x2e, 0x43, 0x61, 0x74, 0x65, 0x67, 0x6f,
	0x72, 0x79, 0x52, 0x08, 0x63, 0x61, 0x74, 0x65, 0x67, 0x6f, 0x72, 0x79, 0x12, 0x31, 0x0a, 0x04,
	0x74, 0x79, 0x70, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x1d, 0x2e, 0x73, 0x61, 0x70,
	0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x6d, 0x65, 0x74,
	0x72, 0x69, 0x63, 0x73, 0x2e, 0x54, 0x79, 0x70, 0x65, 0x52, 0x04, 0x74, 0x79, 0x70, 0x65, 0x12,
	0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x6e,
	0x61, 0x6d, 0x65, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x04, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x12, 0x31, 0x0a, 0x04, 0x75, 0x6e, 0x69,
	0x74, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x1d, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65,
	0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63,
	0x73, 0x2e, 0x55, 0x6e, 0x69, 0x74, 0x52, 0x04, 0x75, 0x6e, 0x69, 0x74, 0x12, 0x3a, 0x0a, 0x07,
	0x63, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x18, 0x06, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x20, 0x2e,
	0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e,
	0x6d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x73, 0x2e, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x52,
	0x07, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x12, 0x21, 0x0a, 0x0c, 0x6c, 0x61, 0x73, 0x74,
	0x5f, 0x72, 0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x18, 0x07, 0x20, 0x01, 0x28, 0x03, 0x52, 0x0b,
	0x6c, 0x61, 0x73, 0x74, 0x52, 0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x12, 0x53, 0x0a, 0x10, 0x72,
	0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x5f, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x76, 0x61, 0x6c, 0x18,
	0x08, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x28, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x73, 0x2e,
	0x52, 0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x49, 0x6e, 0x74, 0x65, 0x72, 0x76, 0x61, 0x6c, 0x52,
	0x0f, 0x72, 0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x49, 0x6e, 0x74, 0x65, 0x72, 0x76, 0x61, 0x6c,
	0x12, 0x1b, 0x0a, 0x09, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x5f, 0x69, 0x64, 0x18, 0x09, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x08, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x49, 0x64, 0x2a, 0x60, 0x0a,
	0x07, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x12, 0x1d, 0x0a, 0x11, 0x43, 0x4f, 0x4e, 0x54,
	0x45, 0x58, 0x54, 0x5f, 0x55, 0x4e, 0x44, 0x45, 0x46, 0x49, 0x4e, 0x45, 0x44, 0x10, 0x00, 0x1a,
	0x06, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x00, 0x12, 0x18, 0x0a, 0x0a, 0x43, 0x4f, 0x4e, 0x54, 0x45,
	0x58, 0x54, 0x5f, 0x56, 0x4d, 0x10, 0x01, 0x1a, 0x08, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x02, 0x76,
	0x6d, 0x12, 0x1c, 0x0a, 0x0c, 0x43, 0x4f, 0x4e, 0x54, 0x45, 0x58, 0x54, 0x5f, 0x48, 0x4f, 0x53,
	0x54, 0x10, 0x02, 0x1a, 0x0a, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x04, 0x68, 0x6f, 0x73, 0x74, 0x2a,
	0xd1, 0x01, 0x0a, 0x08, 0x43, 0x61, 0x74, 0x65, 0x67, 0x6f, 0x72, 0x79, 0x12, 0x1e, 0x0a, 0x12,
	0x43, 0x41, 0x54, 0x45, 0x47, 0x4f, 0x52, 0x59, 0x5f, 0x55, 0x4e, 0x44, 0x45, 0x46, 0x49, 0x4e,
	0x45, 0x44, 0x10, 0x00, 0x1a, 0x06, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x00, 0x12, 0x21, 0x0a, 0x0f,
	0x43, 0x41, 0x54, 0x45, 0x47, 0x4f, 0x52, 0x59, 0x5f, 0x43, 0x4f, 0x4e, 0x46, 0x49, 0x47, 0x10,
	0x01, 0x1a, 0x0c, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x12,
	0x1b, 0x0a, 0x0c, 0x43, 0x41, 0x54, 0x45, 0x47, 0x4f, 0x52, 0x59, 0x5f, 0x43, 0x50, 0x55, 0x10,
	0x02, 0x1a, 0x09, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x03, 0x63, 0x70, 0x75, 0x12, 0x21, 0x0a, 0x0f,
	0x43, 0x41, 0x54, 0x45, 0x47, 0x4f, 0x52, 0x59, 0x5f, 0x4d, 0x45, 0x4d, 0x4f, 0x52, 0x59, 0x10,
	0x03, 0x1a, 0x0c, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x12,
	0x23, 0x0a, 0x10, 0x43, 0x41, 0x54, 0x45, 0x47, 0x4f, 0x52, 0x59, 0x5f, 0x4e, 0x45, 0x54, 0x57,
	0x4f, 0x52, 0x4b, 0x10, 0x04, 0x1a, 0x0d, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x07, 0x6e, 0x65, 0x74,
	0x77, 0x6f, 0x72, 0x6b, 0x12, 0x1d, 0x0a, 0x0d, 0x43, 0x41, 0x54, 0x45, 0x47, 0x4f, 0x52, 0x59,
	0x5f, 0x44, 0x49, 0x53, 0x4b, 0x10, 0x05, 0x1a, 0x0a, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x04, 0x64,
	0x69, 0x73, 0x6b, 0x2a, 0x96, 0x02, 0x0a, 0x04, 0x54, 0x79, 0x70, 0x65, 0x12, 0x1a, 0x0a, 0x0e,
	0x54, 0x59, 0x50, 0x45, 0x5f, 0x55, 0x4e, 0x44, 0x45, 0x46, 0x49, 0x4e, 0x45, 0x44, 0x10, 0x00,
	0x1a, 0x06, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x00, 0x12, 0x1b, 0x0a, 0x0a, 0x54, 0x59, 0x50, 0x45,
	0x5f, 0x49, 0x4e, 0x54, 0x33, 0x32, 0x10, 0x01, 0x1a, 0x0b, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x05,
	0x69, 0x6e, 0x74, 0x33, 0x32, 0x12, 0x1b, 0x0a, 0x0a, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x49, 0x4e,
	0x54, 0x36, 0x34, 0x10, 0x02, 0x1a, 0x0b, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x05, 0x69, 0x6e, 0x74,
	0x36, 0x34, 0x12, 0x1d, 0x0a, 0x0b, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x55, 0x49, 0x4e, 0x54, 0x33,
	0x32, 0x10, 0x03, 0x1a, 0x0c, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x75, 0x69, 0x6e, 0x74, 0x33,
	0x32, 0x12, 0x1d, 0x0a, 0x0b, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x55, 0x49, 0x4e, 0x54, 0x36, 0x34,
	0x10, 0x04, 0x1a, 0x0c, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x75, 0x69, 0x6e, 0x74, 0x36, 0x34,
	0x12, 0x1d, 0x0a, 0x0b, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x52, 0x45, 0x41, 0x4c, 0x33, 0x32, 0x10,
	0x05, 0x1a, 0x0c, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x72, 0x65, 0x61, 0x6c, 0x33, 0x32, 0x12,
	0x1d, 0x0a, 0x0b, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x52, 0x45, 0x41, 0x4c, 0x36, 0x34, 0x10, 0x06,
	0x1a, 0x0c, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x72, 0x65, 0x61, 0x6c, 0x36, 0x34, 0x12, 0x1d,
	0x0a, 0x0b, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x44, 0x4f, 0x55, 0x42, 0x4c, 0x45, 0x10, 0x07, 0x1a,
	0x0c, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x64, 0x6f, 0x75, 0x62, 0x6c, 0x65, 0x12, 0x1d, 0x0a,
	0x0b, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x53, 0x54, 0x52, 0x49, 0x4e, 0x47, 0x10, 0x08, 0x1a, 0x0c,
	0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x06, 0x73, 0x74, 0x72, 0x69, 0x6e, 0x67, 0x2a, 0xfe, 0x02, 0x0a,
	0x04, 0x55, 0x6e, 0x69, 0x74, 0x12, 0x1a, 0x0a, 0x0e, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x55, 0x4e,
	0x44, 0x45, 0x46, 0x49, 0x4e, 0x45, 0x44, 0x10, 0x00, 0x1a, 0x06, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d,
	0x00, 0x12, 0x19, 0x0a, 0x09, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x4e, 0x4f, 0x4e, 0x45, 0x10, 0x01,
	0x1a, 0x0a, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x04, 0x6e, 0x6f, 0x6e, 0x65, 0x12, 0x17, 0x0a, 0x08,
	0x55, 0x4e, 0x49, 0x54, 0x5f, 0x4d, 0x48, 0x5a, 0x10, 0x02, 0x1a, 0x09, 0xd2, 0x8d, 0xe6, 0xdc,
	0x0d, 0x03, 0x4d, 0x48, 0x7a, 0x12, 0x15, 0x0a, 0x07, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x43, 0x55,
	0x10, 0x03, 0x1a, 0x08, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x02, 0x43, 0x55, 0x12, 0x1f, 0x0a, 0x0c,
	0x55, 0x4e, 0x49, 0x54, 0x5f, 0x50, 0x45, 0x52, 0x43, 0x45, 0x4e, 0x54, 0x10, 0x04, 0x1a, 0x0d,
	0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x07, 0x50, 0x65, 0x72, 0x63, 0x65, 0x6e, 0x74, 0x12, 0x15, 0x0a,
	0x07, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x4d, 0x42, 0x10, 0x05, 0x1a, 0x08, 0xd2, 0x8d, 0xe6, 0xdc,
	0x0d, 0x02, 0x4d, 0x42, 0x12, 0x19, 0x0a, 0x09, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x4b, 0x42, 0x50,
	0x53, 0x10, 0x06, 0x1a, 0x0a, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x04, 0x4b, 0x62, 0x70, 0x73, 0x12,
	0x19, 0x0a, 0x09, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x4d, 0x42, 0x50, 0x53, 0x10, 0x07, 0x1a, 0x0a,
	0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x04, 0x4d, 0x62, 0x70, 0x73, 0x12, 0x19, 0x0a, 0x09, 0x55, 0x4e,
	0x49, 0x54, 0x5f, 0x4d, 0x53, 0x45, 0x43, 0x10, 0x08, 0x1a, 0x0a, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d,
	0x04, 0x6d, 0x73, 0x65, 0x63, 0x12, 0x17, 0x0a, 0x08, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x53, 0x45,
	0x43, 0x10, 0x09, 0x1a, 0x09, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x03, 0x73, 0x65, 0x63, 0x12, 0x17,
	0x0a, 0x08, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x42, 0x50, 0x53, 0x10, 0x0a, 0x1a, 0x09, 0xd2, 0x8d,
	0xe6, 0xdc, 0x0d, 0x03, 0x42, 0x70, 0x73, 0x12, 0x25, 0x0a, 0x10, 0x55, 0x4e, 0x49, 0x54, 0x5f,
	0x4f, 0x50, 0x53, 0x5f, 0x50, 0x45, 0x52, 0x5f, 0x53, 0x45, 0x43, 0x10, 0x0b, 0x1a, 0x0f, 0xd2,
	0x8d, 0xe6, 0xdc, 0x0d, 0x09, 0x4f, 0x70, 0x73, 0x50, 0x65, 0x72, 0x53, 0x65, 0x63, 0x12, 0x2d,
	0x0a, 0x14, 0x55, 0x4e, 0x49, 0x54, 0x5f, 0x50, 0x41, 0x43, 0x4b, 0x45, 0x54, 0x53, 0x5f, 0x50,
	0x45, 0x52, 0x5f, 0x53, 0x45, 0x43, 0x10, 0x0c, 0x1a, 0x13, 0xd2, 0x8d, 0xe6, 0xdc, 0x0d, 0x0d,
	0x50, 0x61, 0x63, 0x6b, 0x65, 0x74, 0x73, 0x50, 0x65, 0x72, 0x53, 0x65, 0x63, 0x2a, 0x6d, 0x0a,
	0x0f, 0x52, 0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x49, 0x6e, 0x74, 0x65, 0x72, 0x76, 0x61, 0x6c,
	0x12, 0x1d, 0x0a, 0x19, 0x52, 0x45, 0x46, 0x52, 0x45, 0x53, 0x48, 0x49, 0x4e, 0x54, 0x45, 0x52,
	0x56, 0x41, 0x4c, 0x5f, 0x55, 0x4e, 0x44, 0x45, 0x46, 0x49, 0x4e, 0x45, 0x44, 0x10, 0x00, 0x12,
	0x1b, 0x0a, 0x17, 0x52, 0x45, 0x46, 0x52, 0x45, 0x53, 0x48, 0x49, 0x4e, 0x54, 0x45, 0x52, 0x56,
	0x41, 0x4c, 0x5f, 0x52, 0x45, 0x53, 0x54, 0x41, 0x52, 0x54, 0x10, 0x01, 0x12, 0x1e, 0x0a, 0x1a,
	0x52, 0x45, 0x46, 0x52, 0x45, 0x53, 0x48, 0x49, 0x4e, 0x54, 0x45, 0x52, 0x56, 0x41, 0x4c, 0x5f,
	0x50, 0x45, 0x52, 0x5f, 0x4d, 0x49, 0x4e, 0x55, 0x54, 0x45, 0x10, 0x02, 0x3a, 0x40, 0x0a, 0x08,
	0x78, 0x6d, 0x6c, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x12, 0x21, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c,
	0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x45, 0x6e, 0x75, 0x6d, 0x56,
	0x61, 0x6c, 0x75, 0x65, 0x4f, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x18, 0xda, 0xe1, 0xcc, 0xdb,
	0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x78, 0x6d, 0x6c, 0x4e, 0x61, 0x6d, 0x65, 0x62, 0x06,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_metrics_metrics_proto_rawDescOnce sync.Once
	file_metrics_metrics_proto_rawDescData = file_metrics_metrics_proto_rawDesc
)

func file_metrics_metrics_proto_rawDescGZIP() []byte {
	file_metrics_metrics_proto_rawDescOnce.Do(func() {
		file_metrics_metrics_proto_rawDescData = protoimpl.X.CompressGZIP(file_metrics_metrics_proto_rawDescData)
	})
	return file_metrics_metrics_proto_rawDescData
}

var file_metrics_metrics_proto_enumTypes = make([]protoimpl.EnumInfo, 5)
var file_metrics_metrics_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_metrics_metrics_proto_goTypes = []interface{}{
	(Context)(0),                        // 0: sapagent.protos.metrics.Context
	(Category)(0),                       // 1: sapagent.protos.metrics.Category
	(Type)(0),                           // 2: sapagent.protos.metrics.Type
	(Unit)(0),                           // 3: sapagent.protos.metrics.Unit
	(RefreshInterval)(0),                // 4: sapagent.protos.metrics.RefreshInterval
	(*MetricsCollection)(nil),           // 5: sapagent.protos.metrics.MetricsCollection
	(*Metric)(nil),                      // 6: sapagent.protos.metrics.Metric
	(*descriptor.EnumValueOptions)(nil), // 7: google.protobuf.EnumValueOptions
}
var file_metrics_metrics_proto_depIdxs = []int32{
	6, // 0: sapagent.protos.metrics.MetricsCollection.metrics:type_name -> sapagent.protos.metrics.Metric
	1, // 1: sapagent.protos.metrics.Metric.category:type_name -> sapagent.protos.metrics.Category
	2, // 2: sapagent.protos.metrics.Metric.type:type_name -> sapagent.protos.metrics.Type
	3, // 3: sapagent.protos.metrics.Metric.unit:type_name -> sapagent.protos.metrics.Unit
	0, // 4: sapagent.protos.metrics.Metric.context:type_name -> sapagent.protos.metrics.Context
	4, // 5: sapagent.protos.metrics.Metric.refresh_interval:type_name -> sapagent.protos.metrics.RefreshInterval
	7, // 6: sapagent.protos.metrics.xml_name:extendee -> google.protobuf.EnumValueOptions
	7, // [7:7] is the sub-list for method output_type
	7, // [7:7] is the sub-list for method input_type
	7, // [7:7] is the sub-list for extension type_name
	6, // [6:7] is the sub-list for extension extendee
	0, // [0:6] is the sub-list for field type_name
}

func init() { file_metrics_metrics_proto_init() }
func file_metrics_metrics_proto_init() {
	if File_metrics_metrics_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_metrics_metrics_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MetricsCollection); i {
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
		file_metrics_metrics_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Metric); i {
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
			RawDescriptor: file_metrics_metrics_proto_rawDesc,
			NumEnums:      5,
			NumMessages:   2,
			NumExtensions: 1,
			NumServices:   0,
		},
		GoTypes:           file_metrics_metrics_proto_goTypes,
		DependencyIndexes: file_metrics_metrics_proto_depIdxs,
		EnumInfos:         file_metrics_metrics_proto_enumTypes,
		MessageInfos:      file_metrics_metrics_proto_msgTypes,
		ExtensionInfos:    file_metrics_metrics_proto_extTypes,
	}.Build()
	File_metrics_metrics_proto = out.File
	file_metrics_metrics_proto_rawDesc = nil
	file_metrics_metrics_proto_goTypes = nil
	file_metrics_metrics_proto_depIdxs = nil
}
