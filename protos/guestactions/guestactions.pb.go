//
//Copyright 2024 Google LLC
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
// 	protoc-gen-go v1.35.1
// 	protoc        v3.6.1
// source: guestactions/guestactions.proto

package guestactions

import (
	any1 "github.com/golang/protobuf/ptypes/any"
	_ "github.com/golang/protobuf/ptypes/wrappers"
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

// *
// SapWorkloadAction specifies the type of SAP workload action to perform.
type SapWorkloadAction int32

const (
	SapWorkloadAction_SAP_WORKLOAD_ACTION_UNSPECIFIED SapWorkloadAction = 0
	SapWorkloadAction_SAP_WLM_EVALUATION_FIX          SapWorkloadAction = 1
	SapWorkloadAction_SAP_START                       SapWorkloadAction = 2
	SapWorkloadAction_SAP_STOP                        SapWorkloadAction = 3
	SapWorkloadAction_SAP_SNOOZE                      SapWorkloadAction = 4
)

// Enum value maps for SapWorkloadAction.
var (
	SapWorkloadAction_name = map[int32]string{
		0: "SAP_WORKLOAD_ACTION_UNSPECIFIED",
		1: "SAP_WLM_EVALUATION_FIX",
		2: "SAP_START",
		3: "SAP_STOP",
		4: "SAP_SNOOZE",
	}
	SapWorkloadAction_value = map[string]int32{
		"SAP_WORKLOAD_ACTION_UNSPECIFIED": 0,
		"SAP_WLM_EVALUATION_FIX":          1,
		"SAP_START":                       2,
		"SAP_STOP":                        3,
		"SAP_SNOOZE":                      4,
	}
)

func (x SapWorkloadAction) Enum() *SapWorkloadAction {
	p := new(SapWorkloadAction)
	*p = x
	return p
}

func (x SapWorkloadAction) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SapWorkloadAction) Descriptor() protoreflect.EnumDescriptor {
	return file_guestactions_guestactions_proto_enumTypes[0].Descriptor()
}

func (SapWorkloadAction) Type() protoreflect.EnumType {
	return &file_guestactions_guestactions_proto_enumTypes[0]
}

func (x SapWorkloadAction) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SapWorkloadAction.Descriptor instead.
func (SapWorkloadAction) EnumDescriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{0}
}

// *
// A GuestActionRequest is contained in the body of an UAP message that is sent
// to the agent by the WorkloadActions service.
type GuestActionRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	WorkloadAction *WorkloadAction `protobuf:"bytes,1,opt,name=workload_action,json=workloadAction,proto3" json:"workload_action,omitempty"`
	Commands       []*Command      `protobuf:"bytes,2,rep,name=commands,proto3" json:"commands,omitempty"`
}

func (x *GuestActionRequest) Reset() {
	*x = GuestActionRequest{}
	mi := &file_guestactions_guestactions_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GuestActionRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GuestActionRequest) ProtoMessage() {}

func (x *GuestActionRequest) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GuestActionRequest.ProtoReflect.Descriptor instead.
func (*GuestActionRequest) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{0}
}

func (x *GuestActionRequest) GetWorkloadAction() *WorkloadAction {
	if x != nil {
		return x.WorkloadAction
	}
	return nil
}

func (x *GuestActionRequest) GetCommands() []*Command {
	if x != nil {
		return x.Commands
	}
	return nil
}

// *
// A GuestActionResponse is contained in the body of an Agent Communication
// message that is sent from the agent to the WorkloadActions service.
type GuestActionResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	CommandResults []*CommandResult  `protobuf:"bytes,1,rep,name=command_results,json=commandResults,proto3" json:"command_results,omitempty"`
	Error          *GuestActionError `protobuf:"bytes,2,opt,name=error,proto3" json:"error,omitempty"`
}

func (x *GuestActionResponse) Reset() {
	*x = GuestActionResponse{}
	mi := &file_guestactions_guestactions_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GuestActionResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GuestActionResponse) ProtoMessage() {}

func (x *GuestActionResponse) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GuestActionResponse.ProtoReflect.Descriptor instead.
func (*GuestActionResponse) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{1}
}

func (x *GuestActionResponse) GetCommandResults() []*CommandResult {
	if x != nil {
		return x.CommandResults
	}
	return nil
}

func (x *GuestActionResponse) GetError() *GuestActionError {
	if x != nil {
		return x.Error
	}
	return nil
}

// *
// A WorkloadAction encodes the intended purpose of a guest action request.
// It is intended to be used as metadata for informational purposes.
type WorkloadAction struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to WorkloadType:
	//
	//	*WorkloadAction_SapWorkloadAction
	WorkloadType isWorkloadAction_WorkloadType `protobuf_oneof:"workload_type"`
}

func (x *WorkloadAction) Reset() {
	*x = WorkloadAction{}
	mi := &file_guestactions_guestactions_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *WorkloadAction) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*WorkloadAction) ProtoMessage() {}

func (x *WorkloadAction) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use WorkloadAction.ProtoReflect.Descriptor instead.
func (*WorkloadAction) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{2}
}

func (m *WorkloadAction) GetWorkloadType() isWorkloadAction_WorkloadType {
	if m != nil {
		return m.WorkloadType
	}
	return nil
}

func (x *WorkloadAction) GetSapWorkloadAction() SapWorkloadAction {
	if x, ok := x.GetWorkloadType().(*WorkloadAction_SapWorkloadAction); ok {
		return x.SapWorkloadAction
	}
	return SapWorkloadAction_SAP_WORKLOAD_ACTION_UNSPECIFIED
}

type isWorkloadAction_WorkloadType interface {
	isWorkloadAction_WorkloadType()
}

type WorkloadAction_SapWorkloadAction struct {
	SapWorkloadAction SapWorkloadAction `protobuf:"varint,1,opt,name=sap_workload_action,json=sapWorkloadAction,proto3,enum=sapagent.protos.guestactions.SapWorkloadAction,oneof"`
}

func (*WorkloadAction_SapWorkloadAction) isWorkloadAction_WorkloadType() {}

// *
// Command specifies the type of command to execute.
type Command struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to CommandType:
	//
	//	*Command_AgentCommand
	//	*Command_ShellCommand
	CommandType isCommand_CommandType `protobuf_oneof:"command_type"`
}

func (x *Command) Reset() {
	*x = Command{}
	mi := &file_guestactions_guestactions_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Command) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Command) ProtoMessage() {}

func (x *Command) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Command.ProtoReflect.Descriptor instead.
func (*Command) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{3}
}

func (m *Command) GetCommandType() isCommand_CommandType {
	if m != nil {
		return m.CommandType
	}
	return nil
}

func (x *Command) GetAgentCommand() *AgentCommand {
	if x, ok := x.GetCommandType().(*Command_AgentCommand); ok {
		return x.AgentCommand
	}
	return nil
}

func (x *Command) GetShellCommand() *ShellCommand {
	if x, ok := x.GetCommandType().(*Command_ShellCommand); ok {
		return x.ShellCommand
	}
	return nil
}

type isCommand_CommandType interface {
	isCommand_CommandType()
}

type Command_AgentCommand struct {
	AgentCommand *AgentCommand `protobuf:"bytes,1,opt,name=agent_command,json=agentCommand,proto3,oneof"`
}

type Command_ShellCommand struct {
	ShellCommand *ShellCommand `protobuf:"bytes,2,opt,name=shell_command,json=shellCommand,proto3,oneof"`
}

func (*Command_AgentCommand) isCommand_CommandType() {}

func (*Command_ShellCommand) isCommand_CommandType() {}

// *
// An AgentCommand specifies a one-time executable program for the agent to run.
type AgentCommand struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// command is the name of the agent one-time executable that will be invoked.
	Command string `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	// parameters is a map of key/value pairs that can be used to specify
	// additional one-time executable settings.
	Parameters map[string]string `protobuf:"bytes,2,rep,name=parameters,proto3" json:"parameters,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

func (x *AgentCommand) Reset() {
	*x = AgentCommand{}
	mi := &file_guestactions_guestactions_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *AgentCommand) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AgentCommand) ProtoMessage() {}

func (x *AgentCommand) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AgentCommand.ProtoReflect.Descriptor instead.
func (*AgentCommand) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{4}
}

func (x *AgentCommand) GetCommand() string {
	if x != nil {
		return x.Command
	}
	return ""
}

func (x *AgentCommand) GetParameters() map[string]string {
	if x != nil {
		return x.Parameters
	}
	return nil
}

// *
// A ShellCommand is invoked via the agent's command line executor
type ShellCommand struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// command is the name of the command to be executed.
	Command string `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	// args is a string of arguments to be passed to the command.
	Args string `protobuf:"bytes,2,opt,name=args,proto3" json:"args,omitempty"`
	// Optional. If not specified, the default timeout is 60 seconds.
	TimeoutSeconds int32 `protobuf:"varint,3,opt,name=timeout_seconds,json=timeoutSeconds,proto3" json:"timeout_seconds,omitempty"`
}

func (x *ShellCommand) Reset() {
	*x = ShellCommand{}
	mi := &file_guestactions_guestactions_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ShellCommand) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ShellCommand) ProtoMessage() {}

func (x *ShellCommand) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ShellCommand.ProtoReflect.Descriptor instead.
func (*ShellCommand) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{5}
}

func (x *ShellCommand) GetCommand() string {
	if x != nil {
		return x.Command
	}
	return ""
}

func (x *ShellCommand) GetArgs() string {
	if x != nil {
		return x.Args
	}
	return ""
}

func (x *ShellCommand) GetTimeoutSeconds() int32 {
	if x != nil {
		return x.TimeoutSeconds
	}
	return 0
}

// *
// CommandResult contains the result of a single command execution.
type CommandResult struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Command  *Command  `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	Stdout   string    `protobuf:"bytes,2,opt,name=stdout,proto3" json:"stdout,omitempty"`
	Stderr   string    `protobuf:"bytes,3,opt,name=stderr,proto3" json:"stderr,omitempty"`
	ExitCode int32     `protobuf:"varint,4,opt,name=exit_code,json=exitCode,proto3" json:"exit_code,omitempty"`
	Payload  *any1.Any `protobuf:"bytes,5,opt,name=payload,proto3" json:"payload,omitempty"`
}

func (x *CommandResult) Reset() {
	*x = CommandResult{}
	mi := &file_guestactions_guestactions_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *CommandResult) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CommandResult) ProtoMessage() {}

func (x *CommandResult) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CommandResult.ProtoReflect.Descriptor instead.
func (*CommandResult) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{6}
}

func (x *CommandResult) GetCommand() *Command {
	if x != nil {
		return x.Command
	}
	return nil
}

func (x *CommandResult) GetStdout() string {
	if x != nil {
		return x.Stdout
	}
	return ""
}

func (x *CommandResult) GetStderr() string {
	if x != nil {
		return x.Stderr
	}
	return ""
}

func (x *CommandResult) GetExitCode() int32 {
	if x != nil {
		return x.ExitCode
	}
	return 0
}

func (x *CommandResult) GetPayload() *any1.Any {
	if x != nil {
		return x.Payload
	}
	return nil
}

// *
// GuestActionError contains details about an error that occurred while
// processing a GuestActionRequest.
type GuestActionError struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	ErrorMessage string `protobuf:"bytes,1,opt,name=error_message,json=errorMessage,proto3" json:"error_message,omitempty"`
}

func (x *GuestActionError) Reset() {
	*x = GuestActionError{}
	mi := &file_guestactions_guestactions_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GuestActionError) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GuestActionError) ProtoMessage() {}

func (x *GuestActionError) ProtoReflect() protoreflect.Message {
	mi := &file_guestactions_guestactions_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GuestActionError.ProtoReflect.Descriptor instead.
func (*GuestActionError) Descriptor() ([]byte, []int) {
	return file_guestactions_guestactions_proto_rawDescGZIP(), []int{7}
}

func (x *GuestActionError) GetErrorMessage() string {
	if x != nil {
		return x.ErrorMessage
	}
	return ""
}

var File_guestactions_guestactions_proto protoreflect.FileDescriptor

var file_guestactions_guestactions_proto_rawDesc = []byte{
	0x0a, 0x1f, 0x67, 0x75, 0x65, 0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2f, 0x67,
	0x75, 0x65, 0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x12, 0x1c, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65, 0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x1a,
	0x19, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66,
	0x2f, 0x61, 0x6e, 0x79, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x1e, 0x67, 0x6f, 0x6f, 0x67,
	0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x77, 0x72, 0x61, 0x70,
	0x70, 0x65, 0x72, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xae, 0x01, 0x0a, 0x12, 0x47,
	0x75, 0x65, 0x73, 0x74, 0x41, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73,
	0x74, 0x12, 0x55, 0x0a, 0x0f, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x5f, 0x61, 0x63,
	0x74, 0x69, 0x6f, 0x6e, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x2c, 0x2e, 0x73, 0x61, 0x70,
	0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65,
	0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f,
	0x61, 0x64, 0x41, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x52, 0x0e, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f,
	0x61, 0x64, 0x41, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x41, 0x0a, 0x08, 0x63, 0x6f, 0x6d, 0x6d,
	0x61, 0x6e, 0x64, 0x73, 0x18, 0x02, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x25, 0x2e, 0x73, 0x61, 0x70,
	0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65,
	0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x43, 0x6f, 0x6d, 0x6d, 0x61, 0x6e,
	0x64, 0x52, 0x08, 0x63, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x73, 0x22, 0xb1, 0x01, 0x0a, 0x13,
	0x47, 0x75, 0x65, 0x73, 0x74, 0x41, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x52, 0x65, 0x73, 0x70, 0x6f,
	0x6e, 0x73, 0x65, 0x12, 0x54, 0x0a, 0x0f, 0x63, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x5f, 0x72,
	0x65, 0x73, 0x75, 0x6c, 0x74, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x2b, 0x2e, 0x73,
	0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67,
	0x75, 0x65, 0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x43, 0x6f, 0x6d, 0x6d,
	0x61, 0x6e, 0x64, 0x52, 0x65, 0x73, 0x75, 0x6c, 0x74, 0x52, 0x0e, 0x63, 0x6f, 0x6d, 0x6d, 0x61,
	0x6e, 0x64, 0x52, 0x65, 0x73, 0x75, 0x6c, 0x74, 0x73, 0x12, 0x44, 0x0a, 0x05, 0x65, 0x72, 0x72,
	0x6f, 0x72, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x2e, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67,
	0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65, 0x73, 0x74,
	0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x47, 0x75, 0x65, 0x73, 0x74, 0x41, 0x63, 0x74,
	0x69, 0x6f, 0x6e, 0x45, 0x72, 0x72, 0x6f, 0x72, 0x52, 0x05, 0x65, 0x72, 0x72, 0x6f, 0x72, 0x22,
	0x84, 0x01, 0x0a, 0x0e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x41, 0x63, 0x74, 0x69,
	0x6f, 0x6e, 0x12, 0x61, 0x0a, 0x13, 0x73, 0x61, 0x70, 0x5f, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f,
	0x61, 0x64, 0x5f, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0e, 0x32,
	0x2f, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x73, 0x2e, 0x67, 0x75, 0x65, 0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x53,
	0x61, 0x70, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x41, 0x63, 0x74, 0x69, 0x6f, 0x6e,
	0x48, 0x00, 0x52, 0x11, 0x73, 0x61, 0x70, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x41,
	0x63, 0x74, 0x69, 0x6f, 0x6e, 0x42, 0x0f, 0x0a, 0x0d, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61,
	0x64, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x22, 0xbf, 0x01, 0x0a, 0x07, 0x43, 0x6f, 0x6d, 0x6d, 0x61,
	0x6e, 0x64, 0x12, 0x51, 0x0a, 0x0d, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x5f, 0x63, 0x6f, 0x6d, 0x6d,
	0x61, 0x6e, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x2a, 0x2e, 0x73, 0x61, 0x70, 0x61,
	0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65, 0x73,
	0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x41, 0x67, 0x65, 0x6e, 0x74, 0x43, 0x6f,
	0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x48, 0x00, 0x52, 0x0c, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x43, 0x6f,
	0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x12, 0x51, 0x0a, 0x0d, 0x73, 0x68, 0x65, 0x6c, 0x6c, 0x5f, 0x63,
	0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x2a, 0x2e, 0x73,
	0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67,
	0x75, 0x65, 0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x53, 0x68, 0x65, 0x6c,
	0x6c, 0x43, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x48, 0x00, 0x52, 0x0c, 0x73, 0x68, 0x65, 0x6c,
	0x6c, 0x43, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x42, 0x0e, 0x0a, 0x0c, 0x63, 0x6f, 0x6d, 0x6d,
	0x61, 0x6e, 0x64, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x22, 0xc3, 0x01, 0x0a, 0x0c, 0x41, 0x67, 0x65,
	0x6e, 0x74, 0x43, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x12, 0x18, 0x0a, 0x07, 0x63, 0x6f, 0x6d,
	0x6d, 0x61, 0x6e, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x63, 0x6f, 0x6d, 0x6d,
	0x61, 0x6e, 0x64, 0x12, 0x5a, 0x0a, 0x0a, 0x70, 0x61, 0x72, 0x61, 0x6d, 0x65, 0x74, 0x65, 0x72,
	0x73, 0x18, 0x02, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x3a, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65,
	0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65, 0x73, 0x74, 0x61,
	0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x41, 0x67, 0x65, 0x6e, 0x74, 0x43, 0x6f, 0x6d, 0x6d,
	0x61, 0x6e, 0x64, 0x2e, 0x50, 0x61, 0x72, 0x61, 0x6d, 0x65, 0x74, 0x65, 0x72, 0x73, 0x45, 0x6e,
	0x74, 0x72, 0x79, 0x52, 0x0a, 0x70, 0x61, 0x72, 0x61, 0x6d, 0x65, 0x74, 0x65, 0x72, 0x73, 0x1a,
	0x3d, 0x0a, 0x0f, 0x50, 0x61, 0x72, 0x61, 0x6d, 0x65, 0x74, 0x65, 0x72, 0x73, 0x45, 0x6e, 0x74,
	0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x22, 0x65,
	0x0a, 0x0c, 0x53, 0x68, 0x65, 0x6c, 0x6c, 0x43, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x12, 0x18,
	0x0a, 0x07, 0x63, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x07, 0x63, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x12, 0x12, 0x0a, 0x04, 0x61, 0x72, 0x67, 0x73,
	0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x61, 0x72, 0x67, 0x73, 0x12, 0x27, 0x0a, 0x0f,
	0x74, 0x69, 0x6d, 0x65, 0x6f, 0x75, 0x74, 0x5f, 0x73, 0x65, 0x63, 0x6f, 0x6e, 0x64, 0x73, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x05, 0x52, 0x0e, 0x74, 0x69, 0x6d, 0x65, 0x6f, 0x75, 0x74, 0x53, 0x65,
	0x63, 0x6f, 0x6e, 0x64, 0x73, 0x22, 0xcd, 0x01, 0x0a, 0x0d, 0x43, 0x6f, 0x6d, 0x6d, 0x61, 0x6e,
	0x64, 0x52, 0x65, 0x73, 0x75, 0x6c, 0x74, 0x12, 0x3f, 0x0a, 0x07, 0x63, 0x6f, 0x6d, 0x6d, 0x61,
	0x6e, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x25, 0x2e, 0x73, 0x61, 0x70, 0x61, 0x67,
	0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65, 0x73, 0x74,
	0x61, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x43, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x52,
	0x07, 0x63, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64, 0x12, 0x16, 0x0a, 0x06, 0x73, 0x74, 0x64, 0x6f,
	0x75, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x06, 0x73, 0x74, 0x64, 0x6f, 0x75, 0x74,
	0x12, 0x16, 0x0a, 0x06, 0x73, 0x74, 0x64, 0x65, 0x72, 0x72, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x06, 0x73, 0x74, 0x64, 0x65, 0x72, 0x72, 0x12, 0x1b, 0x0a, 0x09, 0x65, 0x78, 0x69, 0x74,
	0x5f, 0x63, 0x6f, 0x64, 0x65, 0x18, 0x04, 0x20, 0x01, 0x28, 0x05, 0x52, 0x08, 0x65, 0x78, 0x69,
	0x74, 0x43, 0x6f, 0x64, 0x65, 0x12, 0x2e, 0x0a, 0x07, 0x70, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64,
	0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41, 0x6e, 0x79, 0x52, 0x07, 0x70, 0x61,
	0x79, 0x6c, 0x6f, 0x61, 0x64, 0x22, 0x37, 0x0a, 0x10, 0x47, 0x75, 0x65, 0x73, 0x74, 0x41, 0x63,
	0x74, 0x69, 0x6f, 0x6e, 0x45, 0x72, 0x72, 0x6f, 0x72, 0x12, 0x23, 0x0a, 0x0d, 0x65, 0x72, 0x72,
	0x6f, 0x72, 0x5f, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x0c, 0x65, 0x72, 0x72, 0x6f, 0x72, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x2a, 0x81,
	0x01, 0x0a, 0x11, 0x53, 0x61, 0x70, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x41, 0x63,
	0x74, 0x69, 0x6f, 0x6e, 0x12, 0x23, 0x0a, 0x1f, 0x53, 0x41, 0x50, 0x5f, 0x57, 0x4f, 0x52, 0x4b,
	0x4c, 0x4f, 0x41, 0x44, 0x5f, 0x41, 0x43, 0x54, 0x49, 0x4f, 0x4e, 0x5f, 0x55, 0x4e, 0x53, 0x50,
	0x45, 0x43, 0x49, 0x46, 0x49, 0x45, 0x44, 0x10, 0x00, 0x12, 0x1a, 0x0a, 0x16, 0x53, 0x41, 0x50,
	0x5f, 0x57, 0x4c, 0x4d, 0x5f, 0x45, 0x56, 0x41, 0x4c, 0x55, 0x41, 0x54, 0x49, 0x4f, 0x4e, 0x5f,
	0x46, 0x49, 0x58, 0x10, 0x01, 0x12, 0x0d, 0x0a, 0x09, 0x53, 0x41, 0x50, 0x5f, 0x53, 0x54, 0x41,
	0x52, 0x54, 0x10, 0x02, 0x12, 0x0c, 0x0a, 0x08, 0x53, 0x41, 0x50, 0x5f, 0x53, 0x54, 0x4f, 0x50,
	0x10, 0x03, 0x12, 0x0e, 0x0a, 0x0a, 0x53, 0x41, 0x50, 0x5f, 0x53, 0x4e, 0x4f, 0x4f, 0x5a, 0x45,
	0x10, 0x04, 0x42, 0x20, 0x0a, 0x1c, 0x73, 0x61, 0x70, 0x61, 0x67, 0x65, 0x6e, 0x74, 0x2e, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x73, 0x2e, 0x67, 0x75, 0x65, 0x73, 0x74, 0x61, 0x63, 0x74, 0x69, 0x6f,
	0x6e, 0x73, 0x50, 0x01, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_guestactions_guestactions_proto_rawDescOnce sync.Once
	file_guestactions_guestactions_proto_rawDescData = file_guestactions_guestactions_proto_rawDesc
)

func file_guestactions_guestactions_proto_rawDescGZIP() []byte {
	file_guestactions_guestactions_proto_rawDescOnce.Do(func() {
		file_guestactions_guestactions_proto_rawDescData = protoimpl.X.CompressGZIP(file_guestactions_guestactions_proto_rawDescData)
	})
	return file_guestactions_guestactions_proto_rawDescData
}

var file_guestactions_guestactions_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_guestactions_guestactions_proto_msgTypes = make([]protoimpl.MessageInfo, 9)
var file_guestactions_guestactions_proto_goTypes = []any{
	(SapWorkloadAction)(0),      // 0: sapagent.protos.guestactions.SapWorkloadAction
	(*GuestActionRequest)(nil),  // 1: sapagent.protos.guestactions.GuestActionRequest
	(*GuestActionResponse)(nil), // 2: sapagent.protos.guestactions.GuestActionResponse
	(*WorkloadAction)(nil),      // 3: sapagent.protos.guestactions.WorkloadAction
	(*Command)(nil),             // 4: sapagent.protos.guestactions.Command
	(*AgentCommand)(nil),        // 5: sapagent.protos.guestactions.AgentCommand
	(*ShellCommand)(nil),        // 6: sapagent.protos.guestactions.ShellCommand
	(*CommandResult)(nil),       // 7: sapagent.protos.guestactions.CommandResult
	(*GuestActionError)(nil),    // 8: sapagent.protos.guestactions.GuestActionError
	nil,                         // 9: sapagent.protos.guestactions.AgentCommand.ParametersEntry
	(*any1.Any)(nil),            // 10: google.protobuf.Any
}
var file_guestactions_guestactions_proto_depIdxs = []int32{
	3,  // 0: sapagent.protos.guestactions.GuestActionRequest.workload_action:type_name -> sapagent.protos.guestactions.WorkloadAction
	4,  // 1: sapagent.protos.guestactions.GuestActionRequest.commands:type_name -> sapagent.protos.guestactions.Command
	7,  // 2: sapagent.protos.guestactions.GuestActionResponse.command_results:type_name -> sapagent.protos.guestactions.CommandResult
	8,  // 3: sapagent.protos.guestactions.GuestActionResponse.error:type_name -> sapagent.protos.guestactions.GuestActionError
	0,  // 4: sapagent.protos.guestactions.WorkloadAction.sap_workload_action:type_name -> sapagent.protos.guestactions.SapWorkloadAction
	5,  // 5: sapagent.protos.guestactions.Command.agent_command:type_name -> sapagent.protos.guestactions.AgentCommand
	6,  // 6: sapagent.protos.guestactions.Command.shell_command:type_name -> sapagent.protos.guestactions.ShellCommand
	9,  // 7: sapagent.protos.guestactions.AgentCommand.parameters:type_name -> sapagent.protos.guestactions.AgentCommand.ParametersEntry
	4,  // 8: sapagent.protos.guestactions.CommandResult.command:type_name -> sapagent.protos.guestactions.Command
	10, // 9: sapagent.protos.guestactions.CommandResult.payload:type_name -> google.protobuf.Any
	10, // [10:10] is the sub-list for method output_type
	10, // [10:10] is the sub-list for method input_type
	10, // [10:10] is the sub-list for extension type_name
	10, // [10:10] is the sub-list for extension extendee
	0,  // [0:10] is the sub-list for field type_name
}

func init() { file_guestactions_guestactions_proto_init() }
func file_guestactions_guestactions_proto_init() {
	if File_guestactions_guestactions_proto != nil {
		return
	}
	file_guestactions_guestactions_proto_msgTypes[2].OneofWrappers = []any{
		(*WorkloadAction_SapWorkloadAction)(nil),
	}
	file_guestactions_guestactions_proto_msgTypes[3].OneofWrappers = []any{
		(*Command_AgentCommand)(nil),
		(*Command_ShellCommand)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_guestactions_guestactions_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   9,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_guestactions_guestactions_proto_goTypes,
		DependencyIndexes: file_guestactions_guestactions_proto_depIdxs,
		EnumInfos:         file_guestactions_guestactions_proto_enumTypes,
		MessageInfos:      file_guestactions_guestactions_proto_msgTypes,
	}.Build()
	File_guestactions_guestactions_proto = out.File
	file_guestactions_guestactions_proto_rawDesc = nil
	file_guestactions_guestactions_proto_goTypes = nil
	file_guestactions_guestactions_proto_depIdxs = nil
}
