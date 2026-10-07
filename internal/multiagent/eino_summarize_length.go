package multiagent

import (
	"context"
	"errors"
	"fmt"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/components/model"
)

// summaryRecoveryOptions carries the output budget locally without adding legacy
// max_tokens to OpenAI-compatible payloads. Providers ignore this option type.
type summaryRecoveryOptions struct{ outputReserve int }

// summaryLengthError distinguishes a provider-confirmed output limit from other
// incomplete responses. Only this condition permits a compact summary retry.
type summaryLengthError struct{ cause error }

func (e *summaryLengthError) Error() string { return e.cause.Error() }
func (e *summaryLengthError) Unwrap() error { return e.cause }

func isSummaryLengthError(err error) bool {
	var lengthErr *summaryLengthError
	return errors.As(err, &lengthErr)
}

// summaryTextTarget leaves headroom for formatting and provider reasoning within
// the configured output reserve. It is a prompt target, not a tokenizer limit.
func summaryTextTarget(reserve int, compact bool) int {
	if reserve <= 0 {
		reserve = config.DefaultSummarizationOutputReserveTokens
	}
	if compact {
		return max(1, min(1024, reserve/4))
	}
	return max(1, min(2048, reserve/2))
}

func budgetedSummaryInstruction(reserve int) string {
	return einoSummarizeUserInstruction + fmt.Sprintf("\n篇幅预算：全部输出尽量不超过 %d tokens，必须在预算内完整闭合 <summary>；优先保留授权约束、当前任务、核心发现和下一步，合并重复内容，不复制冗长日志。", summaryTextTarget(reserve, false))
}

// generateSummaryWithLengthRecovery retries a truncated generation once using
// the original input plus a shorter-output instruction. It never feeds partial
// output back into history, raises token limits, or mutates the caller's slice.
// Cancellation, transport errors and other incomplete responses return directly;
// the middleware remains responsible for its existing transient-error policy.
func generateSummaryWithLengthRecovery[T any](
	ctx context.Context, input []*T, opts []model.Option,
	generate func(context.Context, []*T, ...model.Option) (*T, error),
	userMessage func(string) *T,
) (*T, error) {
	out, err := generate(ctx, input, opts...)
	if !isSummaryLengthError(err) {
		return out, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, newEinoSummarizationModelError(ctxErr)
	}
	reserve := model.GetImplSpecificOptions(&summaryRecoveryOptions{}, opts...).outputReserve
	if tokens := model.GetCommonOptions(nil, opts...).MaxTokens; tokens != nil {
		reserve = *tokens
	}
	retryInput := append(make([]*T, 0, len(input)+1), input...)
	retryInput = append(retryInput, userMessage(fmt.Sprintf(
		"上一轮摘要触及输出长度上限。请根据同一历史重新生成完整的精简摘要，目标不超过 %d tokens。只输出闭合的 <summary>，省略 <analysis>；保留授权范围、禁止项、当前任务、核心发现和下一步，合并重复项，不抄录长日志。不要续写被截断的文本，不调用工具。", summaryTextTarget(reserve, true))))
	out, err = generate(ctx, retryInput, opts...)
	var exhausted *summaryLengthError
	if errors.As(err, &exhausted) {
		return nil, newEinoSummarizationModelError(fmt.Errorf("摘要达到输出长度上限，精简重试后仍未完成；原始历史未被替换。请核对模型输出限制与 summarization_output_reserve_tokens 配置: %w", exhausted))
	}
	return out, err
}
