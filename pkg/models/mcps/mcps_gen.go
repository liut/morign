// This file is generated - Do Not Edit.

package mcps

import (
	"fmt"

	comm "github.com/cupogo/andvari/models/comm"
	oid "github.com/cupogo/andvari/models/oid"
)

// MCP 传输类型
type TransType int8

const (
	TransTypeStdIO      TransType = 1 + iota //  1 标准IO
	TransTypeSSE                             //  2 SSE
	TransTypeStreamable                      //  3 Streamable
	TransTypeInMemory                        //  4 内部运行
)

func (z *TransType) Decode(s string) error {
	switch s {
	case "1", "stdIO", "StdIO":
		*z = TransTypeStdIO
	case "2", "sse", "SSE":
		*z = TransTypeSSE
	case "3", "streamable", "Streamable", "http", "HTTP":
		*z = TransTypeStreamable
	case "4", "inMemory", "InMemory":
		*z = TransTypeInMemory
	default:
		return fmt.Errorf("invalid transType: %q", s)
	}
	return nil
}
func (z *TransType) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z TransType) String() string {
	switch z {
	case TransTypeStdIO:
		return "stdIO"
	case TransTypeSSE:
		return "sse"
	case TransTypeStreamable:
		return "streamable"
	case TransTypeInMemory:
		return "inMemory"
	default:
		return fmt.Sprintf("transType %d", int8(z))
	}
}
func (z TransType) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// 状态 用于表示连接
type Status int8

const (
	StatusDisconnected Status = 0 + iota //  0 断开 初始默认
	StatusConnecting                     //  1 连接中
	StatusConnected                      //  2 已连接
)

func (z *Status) Decode(s string) error {
	switch s {
	case "0", "disconnected", "Disconnected":
		*z = StatusDisconnected
	case "1", "connecting", "Connecting":
		*z = StatusConnecting
	case "2", "connected", "Connected":
		*z = StatusConnected
	default:
		return fmt.Errorf("invalid status: %q", s)
	}
	return nil
}
func (z *Status) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z Status) String() string {
	switch z {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	default:
		return fmt.Sprintf("status %d", int8(z))
	}
}
func (z Status) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// 头类型
type HeaderCate int8

const (
	HeaderCateAuthorization HeaderCate = 1 << iota //   1 Authorization
	HeaderCateOwnerID                              //   2 OwnerID
	HeaderCateSessionID                            //   4 SessionID

	HeaderCateNone HeaderCate = 0 // None
)

func (z *HeaderCate) Decode(s string) error {
	switch s {
	case "0", "none":
		*z = HeaderCateNone
	case "1", "authorization", "Authorization":
		*z = HeaderCateAuthorization
	case "2", "ownerID", "OwnerID":
		*z = HeaderCateOwnerID
	case "4", "sessionID", "SessionID":
		*z = HeaderCateSessionID
	default:
		return fmt.Errorf("invalid headerCate: %q", s)
	}
	return nil
}
func (z *HeaderCate) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z HeaderCate) String() string {
	switch z {
	case HeaderCateNone:
		return "none"
	case HeaderCateAuthorization:
		return "authorization"
	case HeaderCateOwnerID:
		return "ownerID"
	case HeaderCateSessionID:
		return "sessionID"
	default:
		return fmt.Sprintf("headerCate %d", int8(z))
	}
}
func (z HeaderCate) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// consts of Server 服务器
const (
	ServerTable = "mcp_server"
	ServerAlias = "ms"
	ServerLabel = "server"
	ServerTypID = "mcpsServer"
)

// Server 服务器
type Server struct {
	comm.BaseModel `bun:"table:mcp_server,alias:ms" json:"-"`

	comm.DefaultModel

	ServerBasic

	comm.MetaField
} // @name mcpsServer

type ServerBasic struct {
	// 名称
	Name string `binding:"required" bson:"name" bun:",notnull,unique,type:name" extensions:"x-order=A" form:"name" json:"name" pg:",notnull,unique,type:name"`
	// 传输类型
	//  * `stdIO` - 标准IO
	//  * `sse`
	//  * `streamable`
	//  * `inMemory` - 内部运行
	TransType TransType `bson:"transType" bun:",notnull,type:smallint" enums:"stdIO,sse,streamable,inMemory" extensions:"x-order=B" form:"transType" json:"transType" pg:",notnull,type:smallint" swaggertype:"string"`
	// 指令 仅对 TransType 为 StdIO 时有效
	Command string `bson:"command" bun:",notnull" extensions:"x-order=C" form:"command" json:"command" pg:",notnull"`
	// 完整网址 仅对 TransType 为 SSE 或 HTTP 时有效
	URL string `bson:"url" bun:",notnull" extensions:"x-order=D" form:"url" json:"url" pg:",notnull"`
	// 是否激活
	IsActive bool `bson:"isActive" bun:",notnull" extensions:"x-order=E" form:"isActive" json:"isActive" pg:",notnull"`
	// 连接状态
	//  * `disconnected` - 断开
	//  * `connecting` - 连接中
	//  * `connected` - 已连接
	Status Status `bson:"status" bun:",notnull,type:smallint" enums:"disconnected,connecting,connected" extensions:"x-order=F" form:"status" json:"status" pg:",notnull,type:smallint" swaggertype:"string"`
	// 备注
	Remark string `bson:"remark" bun:",notnull" extensions:"x-order=G" form:"remark" json:"remark" pg:",notnull"`
	// 头分类
	//  * `authorization`
	//  * `ownerID`
	//  * `sessionID`
	HeaderCate HeaderCate `bson:"headerCate" bun:",notnull,type:smallint" enums:"authorization,ownerID,sessionID" extensions:"x-order=H" json:"headerCate" pg:",notnull,type:smallint" swaggertype:"string"`
	// 定制头函数
	HeaderFunc HeaderFunc `bson:"-" bun:"-" extensions:"x-order=I" json:"-" pg:"-"`
	// 频道名，非空时该 server 为频道专属
	Channel string `bson:"-" bun:"-" extensions:"x-order=J" form:"channel" json:"channel,omitempty" pg:"-"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name mcpsServerBasic

type Servers []Server

// Creating function call to it's inner fields defined hooks
func (z *Server) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtFile))
	}

	return z.DefaultModel.Creating()
}
func NewServerWithBasic(in ServerBasic) *Server {
	obj := &Server{
		ServerBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewServerWithID(id any) *Server {
	obj := new(Server)
	_ = obj.SetID(id)
	return obj
}
func (_ *Server) IdentityLabel() string { return ServerLabel }
func (_ *Server) IdentityModel() string { return ServerTypID }
func (_ *Server) IdentityTable() string { return ServerTable }
func (_ *Server) IdentityAlias() string { return ServerAlias }

type ServerSet struct {
	// 名称
	Name *string `extensions:"x-order=A" json:"name"`
	// 传输类型
	//  * `stdIO` - 标准IO
	//  * `sse`
	//  * `streamable`
	//  * `inMemory` - 内部运行
	TransType *TransType `enums:"stdIO,sse,streamable,inMemory" extensions:"x-order=B" json:"transType" swaggertype:"string"`
	// 指令 仅对 TransType 为 StdIO 时有效
	Command *string `extensions:"x-order=C" json:"command"`
	// 完整网址 仅对 TransType 为 SSE 或 HTTP 时有效
	URL *string `extensions:"x-order=D" json:"url"`
	// 是否激活
	IsActive *bool `extensions:"x-order=E" json:"isActive"`
	// 连接状态
	//  * `disconnected` - 断开
	//  * `connecting` - 连接中
	//  * `connected` - 已连接
	Status *Status `enums:"disconnected,connecting,connected" extensions:"x-order=F" json:"status" swaggertype:"string"`
	// 备注
	Remark *string `extensions:"x-order=G" json:"remark"`
	// 头分类
	//  * `authorization`
	//  * `ownerID`
	//  * `sessionID`
	HeaderCate *HeaderCate `enums:"authorization,ownerID,sessionID" extensions:"x-order=H" json:"headerCate" swaggertype:"string"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name mcpsServerSet

func (z *Server) SetWith(o ServerSet) {
	if o.Name != nil && z.Name != *o.Name {
		z.LogChangeValue("name", z.Name, o.Name)
		z.Name = *o.Name
	}
	if o.TransType != nil && z.TransType != *o.TransType {
		z.LogChangeValue("trans_type", z.TransType, o.TransType)
		z.TransType = *o.TransType
	}
	if o.Command != nil && z.Command != *o.Command {
		z.LogChangeValue("command", z.Command, o.Command)
		z.Command = *o.Command
	}
	if o.URL != nil && z.URL != *o.URL {
		z.LogChangeValue("url", z.URL, o.URL)
		z.URL = *o.URL
	}
	if o.IsActive != nil && z.IsActive != *o.IsActive {
		z.LogChangeValue("is_active", z.IsActive, o.IsActive)
		z.IsActive = *o.IsActive
	}
	if o.Status != nil && z.Status != *o.Status {
		z.LogChangeValue("status", z.Status, o.Status)
		z.Status = *o.Status
	}
	if o.Remark != nil && z.Remark != *o.Remark {
		z.LogChangeValue("remark", z.Remark, o.Remark)
		z.Remark = *o.Remark
	}
	if o.HeaderCate != nil {
		z.LogChangeValue("header_cate", z.HeaderCate, o.HeaderCate)
		z.HeaderCate = *o.HeaderCate
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *ServerBasic) MetaAddKVs(args ...any) *ServerBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *ServerSet) MetaAddKVs(args ...any) *ServerSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
