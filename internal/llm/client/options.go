package client

import "context"

type ThinkingConfig struct {
	Type         string
	BudgetTokens int64
}

type OutputConfig struct {
	Effort string
}

type RequestOptions struct {
	Thinking     *ThinkingConfig
	OutputConfig *OutputConfig
	Source       string
}

type requestOptionsContextKey struct{}

func WithRequestOptions(ctx context.Context, opts RequestOptions) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestOptionsContextKey{}, opts)
}

func requestOptionsFromContext(ctx context.Context) (RequestOptions, bool) {
	if ctx == nil {
		return RequestOptions{}, false
	}
	opts, ok := ctx.Value(requestOptionsContextKey{}).(RequestOptions)
	return opts, ok
}
