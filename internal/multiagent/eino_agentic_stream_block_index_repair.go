package multiagent

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// agenticStreamBlockIndexRepairModel separates provider indices reused across
// content types. Each stream owns its mapping; generation and errors pass through.
type agenticStreamBlockIndexRepairModel struct{ base model.AgenticModel }

func newAgenticStreamBlockIndexRepairModel(base model.AgenticModel) model.AgenticModel {
	if base == nil {
		return nil
	}
	return &agenticStreamBlockIndexRepairModel{base: base}
}

// Generate delegates non-streaming requests without changing messages or errors.
func (m *agenticStreamBlockIndexRepairModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return m.base.Generate(ctx, input, opts...)
}

// Stream assigns contiguous indices in first-appearance order. Keys include the
// exact content type, so reasoning, text and tool fragments cannot merge together.
// Repeated fragments retain their index; source messages are never mutated.
func (m *agenticStreamBlockIndexRepairModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	stream, err := m.base.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	indices := make(map[agenticBlockKey]int)
	return schema.StreamReaderWithConvert(stream, func(message *schema.AgenticMessage) (*schema.AgenticMessage, error) {
		return normalizeAgenticBlockIndices(message, indices), nil
	}), nil
}

type agenticBlockKey struct {
	index int
	kind  schema.ContentBlockType
}

func normalizeAgenticBlockIndices(message *schema.AgenticMessage, indices map[agenticBlockKey]int) *schema.AgenticMessage {
	if message == nil {
		return nil
	}
	var output *schema.AgenticMessage
	for position, block := range message.ContentBlocks {
		if block == nil || block.StreamingMeta == nil {
			continue
		}
		key := agenticBlockKey{index: block.StreamingMeta.Index, kind: block.Type}
		index, exists := indices[key]
		if !exists {
			index = len(indices)
			indices[key] = index
		}
		if index == block.StreamingMeta.Index {
			continue
		}
		if output == nil {
			copyMessage := *message
			copyMessage.ContentBlocks = append([]*schema.ContentBlock(nil), message.ContentBlocks...)
			output = &copyMessage
		}
		copyBlock, copyMeta := *block, *block.StreamingMeta
		copyMeta.Index = index
		copyBlock.StreamingMeta = &copyMeta
		output.ContentBlocks[position] = &copyBlock
	}
	if output != nil {
		return output
	}
	return message
}
