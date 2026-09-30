package corpus

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type JSONSchema struct {
	Type                 string                `json:"type,omitempty"`
	Description          string                `json:"description,omitempty"`
	Enum                 []string              `json:"enum,omitempty"`
	Properties           map[string]JSONSchema `json:"properties,omitempty"`
	Required             []string              `json:"required,omitempty"`
	Items                *JSONSchema           `json:"items,omitempty"`
	AdditionalProperties any                   `json:"additionalProperties,omitempty"`
	Default              any                   `json:"default,omitempty"`
	Minimum              *float64              `json:"minimum,omitempty"`
	Maximum              *float64              `json:"maximum,omitempty"`
}

type ChatCompletionRequest struct {
	Model               string                  `json:"model"`
	Messages            []ChatCompletionMessage `json:"messages"`
	MaxCompletionTokens int                     `json:"max_completion_tokens,omitempty"`
	Temperature         float32                 `json:"temperature,omitempty"`
	Tools               []ChatTool              `json:"tools,omitempty"`
	ToolChoice          any                     `json:"tool_choice,omitempty"`
	ParallelToolCalls   any                     `json:"parallel_tool_calls,omitempty"`
	Stream              bool                    `json:"stream,omitempty"`
}

type ChatTool struct {
	Type     string                  `json:"type"`
	Function *ChatFunctionDefinition `json:"function,omitempty"`
}

type ChatFunctionDefinition struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Strict      bool       `json:"strict,omitempty"`
	Parameters  JSONSchema `json:"parameters"`
}

type ChatCompletionMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is a call of a tool. Arguments is the JSON of the call, which the
// SDKs keep as it is.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ChatCompletionResponse is the answer of the model. A client encodes a request
// which is much larger than the response it decodes, so the response is its own
// payload.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int                   `json:"index"`
	Message      ChatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

var llmWords = []string{"scanner", "opcode", "tape", "frame", "cursor", "segment", "payload", "binding"}

// llmSource is a text of the shape of a source file, which the JSON of it
// escapes: quotes, backslashes, tabs and newlines. It is what makes the
// payload escape-heavy, which is the part of the decoding that the libraries
// disagree on the most.
func llmSource(size int) string {
	const chunk = "func (s *%s) scan(off int) (int, error) {\n" +
		"\tfor off < len(s.buf) {\n" +
		"\t\tswitch c := s.buf[off]; c {\n" +
		"\t\tcase '\"', '\\\\':\n" +
		"\t\t\treturn off, &SyntaxError{msg: \"bad byte %%q in %%s\", at: off}\n" +
		"\t\tcase '{', '[':\n" +
		"\t\t\ts.depth++\n" +
		"\t\t}\n" +
		"\t\toff += s.step(off)\n" +
		"\t}\n" +
		"\treturn off, nil\n" +
		"}\n\n"
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, chunk, llmWords[i%len(llmWords)])
	}
	return b.String()
}

// llmNotes is a text of the shape of a system prompt: markdown, with headings
// and lists.
func llmNotes(size int) string {
	const chunk = "### Working with the %s\n\n" +
		"- Read the files before you answer; a guess about the %s is worse than a call.\n" +
		"- One turn holds **one** call of a tool; do not describe a call in prose.\n" +
		"- Keep a change as small as it can be.\n\n"
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, chunk, llmWords[i%len(llmWords)], llmWords[(i+3)%len(llmWords)])
	}
	return b.String()
}

// llmProse is a text of the shape of a message of a user or of the model.
func llmProse(size int) string {
	const chunk = "The %s spends more time in the %s than it did last week, but only for a " +
		"payload with a long %s. Where does it go, and how would you measure it?\n\n"
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, chunk, llmWords[i%len(llmWords)], llmWords[(i+2)%len(llmWords)], llmWords[(i+4)%len(llmWords)])
	}
	return b.String()
}

func llmFloat(v float64) *float64 { return &v }

// llmTools are the definitions of the five tools of the session. The parameters
// of every tool is an object: a map[string]JSONSchema of three or four
// properties, which is where the map of 144-byte values is.
func llmTools() []ChatTool {
	str := func(description string) JSONSchema {
		return JSONSchema{Type: "string", Description: description}
	}
	integer := func(description string) JSONSchema {
		return JSONSchema{Type: "integer", Description: description}
	}
	boolean := func(description string) JSONSchema {
		return JSONSchema{Type: "boolean", Description: description}
	}
	params := func(required []string, properties map[string]JSONSchema) JSONSchema {
		return JSONSchema{Type: "object", Properties: properties, Required: required, AdditionalProperties: false}
	}

	timeout := integer("Give up after this many milliseconds")
	timeout.Minimum, timeout.Maximum = llmFloat(1), llmFloat(600000)
	replaceAll := boolean("Substitute every occurrence, not only the first")
	replaceAll.Default = false
	shell := str("Which shell runs the command line")
	shell.Enum = []string{"bash", "zsh", "sh"}
	paths := JSONSchema{Type: "array", Description: "Several paths at once, instead of one"}
	paths.Items = &JSONSchema{Type: "string", Description: "One path of the list"}

	return []ChatTool{
		{Type: "function", Function: &ChatFunctionDefinition{
			Name:        "read_file",
			Description: "Returns the contents of a file of the repository. A long file can be read by parts with offset and limit.",
			Parameters: params([]string{"path"}, map[string]JSONSchema{
				"path":   str("A path relative to the root of the repository"),
				"offset": integer("The first line to return, counted from 1"),
				"limit":  integer("How many lines to return at most"),
			})}},
		{Type: "function", Function: &ChatFunctionDefinition{
			Name:        "edit_file",
			Description: "Substitutes one snippet of a file by another. The snippet has to appear exactly once unless replace_all is set.",
			Parameters: params([]string{"path", "old_string", "new_string"}, map[string]JSONSchema{
				"path":        str("A path relative to the root of the repository"),
				"old_string":  str("The snippet to look for, which has to match the file byte for byte"),
				"new_string":  str("The snippet to write in its place"),
				"replace_all": replaceAll,
			})}},
		{Type: "function", Function: &ChatFunctionDefinition{
			Name:        "search",
			Description: "Looks for a regular expression in the files of the repository, and reports every matching line with its path.",
			Parameters: params([]string{"pattern"}, map[string]JSONSchema{
				"pattern":          str("A regular expression in the syntax of regexp"),
				"path":             str("Look only inside this directory"),
				"glob":             str("Restrict the files by a pattern, such as *.go"),
				"case_insensitive": boolean("Ignore the case of the letters"),
			})}},
		{Type: "function", Function: &ChatFunctionDefinition{
			Name:        "run_command",
			Description: "Executes a command line at the root of the repository, and returns its output and its exit status.",
			Parameters: params([]string{"command"}, map[string]JSONSchema{
				"command":     str("The command line to execute"),
				"description": str("One line on what the command is for"),
				"shell":       shell,
				"timeout":     timeout,
			})}},
		{Type: "function", Function: &ChatFunctionDefinition{
			Name:        "list_files",
			Description: "Lists the entries of a directory of the repository, without descending into the subdirectories.",
			Parameters: params([]string{"path"}, map[string]JSONSchema{
				"path":     str("The directory to list, relative to the root of the repository"),
				"paths":    paths,
				"absolute": boolean("Print the absolute path of every entry"),
			})}},
	}
}

func llmCall(id, name string, arguments string) ToolCall {
	return ToolCall{
		ID:       id,
		Type:     "function",
		Function: FunctionCall{Name: name, Arguments: json.RawMessage(arguments)},
	}
}

// llmToolsRequest is the smallest payload which has the map of 144-byte values:
// the definitions of the five tools, and nothing else. It isolates the encoding
// and the decoding of map[string]JSONSchema from the strings of the rest of the
// session.
func llmToolsRequest() *ChatCompletionRequest {
	return &ChatCompletionRequest{
		Model:               "velox-5-code",
		Messages:            []ChatCompletionMessage{{Role: "user", Content: llmProse(260)}},
		Tools:               llmTools(),
		MaxCompletionTokens: 4096,
		Temperature:         0.2,
	}
}

// llmRequest is one turn of the session: the tools, the request of the user,
// five calls of the tools with their results, one of which is a source file of
// 15 KB, and the answer of the model.
func llmRequest() *ChatCompletionRequest {
	calls := []ToolCall{
		llmCall("call_01", "search", `{"pattern":"func \\(s \\*scanner\\) scan","path":"vdec"}`),
		llmCall("call_02", "read_file", `{"path":"vdec/string.go","offset":160,"limit":420}`),
		llmCall("call_03", "edit_file", `{"path":"vdec/string.go","old_string":"off++","new_string":"off += n"}`),
		llmCall("call_04", "run_command", `{"command":"go test ./vdec/","shell":"bash","timeout":120000}`),
		llmCall("call_05", "list_files", `{"path":"venc","absolute":false}`),
	}
	names := []string{"search", "read_file", "edit_file", "run_command", "list_files"}
	results := []string{
		llmSource(500),
		llmSource(15000),
		"vdec/string.go: the substitution is written.\n",
		llmProse(2000) + "PASS\nok  \tgithub.com/velox-io/json/vdec\t0.318s\n",
		llmProse(600),
	}

	messages := make([]ChatCompletionMessage, 0, 2*len(calls)+3)
	messages = append(messages,
		ChatCompletionMessage{Role: "system", Content: llmNotes(1400)},
		ChatCompletionMessage{Role: "user", Content: llmProse(260)},
		ChatCompletionMessage{Role: "assistant", Content: llmProse(220), ToolCalls: calls},
	)
	for i, name := range names {
		messages = append(messages, ChatCompletionMessage{
			Role:       "tool",
			ToolCallID: calls[i].ID,
			Name:       name,
			Content:    results[i],
		})
	}
	messages = append(messages, ChatCompletionMessage{
		Role:    "assistant",
		Content: llmProse(900),
		ToolCalls: []ToolCall{
			llmCall("call_06", "run_command", `{"command":"go test ./...","shell":"zsh","timeout":600000}`),
		},
	})
	return &ChatCompletionRequest{
		Model:               "velox-5-code",
		Messages:            messages,
		Tools:               llmTools(),
		MaxCompletionTokens: 8192,
		Temperature:         0.2,
		ParallelToolCalls:   true,
	}
}

// llmResponse is the answer of the model to llmRequest.
func llmResponse() *ChatCompletionResponse {
	return &ChatCompletionResponse{
		ID:      "chatcmpl-7Fq2Lm9vXs4Tb1Rd8NkPzA",
		Object:  "chat.completion",
		Created: 1759123456,
		Model:   "velox-5-code",
		Choices: []Choice{{
			Index: 0,
			Message: ChatCompletionMessage{
				Role:    "assistant",
				Content: llmProse(700),
				ToolCalls: []ToolCall{
					llmCall("call_B3", "edit_file", `{"path":"vdec/string.go","old_string":"off++","new_string":"off += n"}`),
				},
			},
			FinishReason: "tool_calls",
		}},
		Usage: Usage{PromptTokens: 18432, CompletionTokens: 512, TotalTokens: 18944},
	}
}

// Every payload is built once, on the first use.

var (
	llmToolsOnce  sync.Once
	llmToolsData  []byte
	llmRequestOnc sync.Once
	llmRequestDta []byte
	llmResponseOn sync.Once
	llmResponseDt []byte
)

func mustIndent(v any) []byte {
	b, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		panic("corpus: marshal the llm payload: " + err.Error())
	}
	return b
}

// LLMToolsJSON is the JSON of the definitions of the five tools of the session.
func LLMToolsJSON() []byte {
	llmToolsOnce.Do(func() { llmToolsData = mustIndent(llmToolsRequest()) })
	return llmToolsData
}

// LLMRequestJSON is the JSON of one turn of the session: the tools, five calls
// and their results, one of which is a source file of 15 KB.
func LLMRequestJSON() []byte {
	llmRequestOnc.Do(func() { llmRequestDta = mustIndent(llmRequest()) })
	return llmRequestDta
}

// LLMResponseJSON is the JSON of the answer of the model.
func LLMResponseJSON() []byte {
	llmResponseOn.Do(func() { llmResponseDt = mustIndent(llmResponse()) })
	return llmResponseDt
}
