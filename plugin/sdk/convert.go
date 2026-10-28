package sdk

import (
	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// toProtoRef 把驱动侧文件引用转成跨进程消息（字段一一对应）
func toProtoRef(ref types.FileRef) *plugin.FileRef {
	return &plugin.FileRef{
		LocationId:     proto.Uint64(ref.LocationID),
		ProviderFileId: proto.String(ref.ProviderFileID),
		Name:           proto.String(ref.Name),
		Path:           proto.String(ref.Path),
		Size:           proto.Uint64(ref.Size),
		ChunkSize:      proto.Uint64(ref.ChunkSize),
		Hash:           proto.String(ref.Hash),
		ChunkHash:      proto.String(ref.ChunkHash),
		ChunkNumber:    proto.Uint32(ref.ChunkNumber),
		ChannelId:      proto.String(ref.ChannelID),
		MessageId:      proto.Int32(int32(ref.MessageID)),
		CacheStatus:    ref.CacheStatus.Enum(),
	}
}

// toProtoRefs 批量转换
func toProtoRefs(refs []types.FileRef) []*plugin.FileRef {
	if refs == nil {
		return nil
	}
	out := make([]*plugin.FileRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, toProtoRef(ref))
	}
	return out
}

// fromProtoRef 把跨进程消息还原成驱动侧文件引用
func fromProtoRef(ref *plugin.FileRef) types.FileRef {
	return types.FileRef{
		LocationID:     ref.GetLocationId(),
		ProviderFileID: ref.GetProviderFileId(),
		Name:           ref.GetName(),
		Path:           ref.GetPath(),
		Size:           ref.GetSize(),
		ChunkSize:      ref.GetChunkSize(),
		Hash:           ref.GetHash(),
		ChunkHash:      ref.GetChunkHash(),
		ChunkNumber:    ref.GetChunkNumber(),
		ChannelID:      ref.GetChannelId(),
		MessageID:      int(ref.GetMessageId()),
		CacheStatus:    ref.GetCacheStatus(),
	}
}

// fromProtoRefs 批量还原
func fromProtoRefs(refs []*plugin.FileRef) []types.FileRef {
	if refs == nil {
		return nil
	}
	out := make([]types.FileRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, fromProtoRef(ref))
	}
	return out
}
