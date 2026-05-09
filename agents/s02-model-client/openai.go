package s02

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIChatCompletions calls /v1/chat/completions with stream=true.
//
// Two ways to construct:
//   - NewOpenAI(apiKey)         — production: real OpenAI endpoint
//   - WithEndpoint(url, apiKey) — testing: point at a fake server
type OpenAIChatCompletions struct {
	APIKey   string
	Endpoint string // default https://api.openai.com/v1
	HTTP     *http.Client
}

func NewOpenAI(apiKey string) *OpenAIChatCompletions {
	return &OpenAIChatCompletions{
		APIKey:   apiKey,
		Endpoint: "https://api.openai.com/v1",
		HTTP:     http.DefaultClient,
	}
}

func WithEndpoint(url, apiKey string) *OpenAIChatCompletions {
	return &OpenAIChatCompletions{
		APIKey:   apiKey,
		Endpoint: url,
		HTTP:     http.DefaultClient,
	}
}

type openAIRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolSchema  `json:"tools,omitempty"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
}

// Stream issues the request and forwards SSE events to `out` until the stream
// ends or the context is cancelled. The caller is responsible for closing `out`.
func (c *OpenAIChatCompletions) Stream(ctx context.Context, req ProviderRequest, out chan<- ProviderEvent) error {
	body, err := json.Marshal(openAIRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		Stream:      true,
		Temperature: req.Temperature,
	})
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("content-type", "application/json")
	hreq.Header.Set("accept", "text/event-stream")
	if c.APIKey != "" {
		hreq.Header.Set("authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(b))
	}

	return parseSSE(resp.Body, out)
}

// parseSSE reads `data: …` lines until `data: [DONE]`. Each non-DONE payload
// is a ChatCompletionChunk; we accumulate streaming tool-call arguments
// across chunks (OpenAI splits them token-by-token).
func parseSSE(r io.Reader, out chan<- ProviderEvent) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB max line — long tool args

	// in-flight tool calls keyed by index (OpenAI's chunk-merge contract)
	toolByIndex := map[int]*ToolCall{}
	var argsBuf = map[int]*strings.Builder{}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name string `json:"name"`
							Args string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			out <- ProvError{Err: fmt.Errorf("sse: bad json: %w (line=%q)", err, payload)}
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]

		if ch.Delta.Content != "" {
			out <- ProvText{Delta: ch.Delta.Content}
		}

		for _, tc := range ch.Delta.ToolCalls {
			c, ok := toolByIndex[tc.Index]
			if !ok {
				c = &ToolCall{Type: "function"}
				toolByIndex[tc.Index] = c
				argsBuf[tc.Index] = &strings.Builder{}
			}
			if tc.ID != "" {
				c.ID = tc.ID
			}
			if tc.Function.Name != "" {
				c.Function.Name = tc.Function.Name
			}
			if tc.Function.Args != "" {
				argsBuf[tc.Index].WriteString(tc.Function.Args)
			}
		}

		if ch.FinishReason != "" {
			// flush completed tool calls in stable index order
			for i := 0; i <= len(toolByIndex); i++ {
				c, ok := toolByIndex[i]
				if !ok {
					continue
				}
				c.Function.Args = json.RawMessage(argsBuf[i].String())
				out <- ProvToolCall{Call: *c}
			}
			out <- ProvDone{FinishReason: ch.FinishReason}
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
