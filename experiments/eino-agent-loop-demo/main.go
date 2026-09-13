package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// LoadMediaParams mirrors the OpenStoryline tool argument seen in the real trace:
//
//	{"mode":"auto"}
type LoadMediaParams struct {
	Mode string `json:"mode" jsonschema:"description=auto: load media automatically; skip: skip loading; default: default behavior"`
}

func storylineLoadMedia(_ context.Context, params *LoadMediaParams) (string, error) {
	if params.Mode == "" {
		params.Mode = "auto"
	}

	result := map[string]any{
		"summary": map[string]any{
			"node_summary": map[string]any{
				"ERROR":        "",
				"WARNING":      "",
				"INFO_USER":    "Media indexing completed successfully: 3 video(s), 0 image(s)",
				"preview_urls": []string{},
				"artifact_id":  "load_media_demo",
			},
			"tool_execute_result": map[string]any{},
		},
		"isError": false,
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("missing environment variable %s", name)
	}
	return value
}

func main() {
	ctx := context.Background()

	// 1. Register a tool. In OpenStoryline this comes from MCP; here it is a local mock.
	loadMediaTool, err := utils.InferTool(
		"storyline_load_media",
		"Loads and indexes input media. Entry point with no dependencies; required by all downstream operations",
		storylineLoadMedia,
	)
	if err != nil {
		log.Fatal(err)
	}

	tools := []tool.BaseTool{loadMediaTool}

	// 2. Create an OpenAI-compatible ChatModel. DeepSeek works by setting OPENAI_BASE_URL.
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: mustEnv("OPENAI_BASE_URL"),
		APIKey:  mustEnv("OPENAI_API_KEY"),
		Model:   mustEnv("OPENAI_MODEL"),
	})
	if err != nil {
		log.Fatal(err)
	}

	// 3. Bind tool schemas to the model.
	toolInfos := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			log.Fatal(err)
		}
		toolInfos = append(toolInfos, info)
	}
	if err := chatModel.BindTools(toolInfos); err != nil {
		log.Fatal(err)
	}

	// 4. Build a ToolsNode. It maps tool_calls.name to the real Go tool implementation.
	toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{
		Tools: tools,
	})
	if err != nil {
		log.Fatal(err)
	}

	// 5. Compose a minimal one-step agent:
	//    model reads messages+tools, returns assistant content/tool_calls,
	//    tools node executes the returned tool_calls.
	chain := compose.NewChain[[]*schema.Message, []*schema.Message]()
	chain.
		AppendChatModel(chatModel, compose.WithNodeName("chat_model")).
		AppendToolsNode(toolsNode, compose.WithNodeName("tools"))

	agent, err := chain.Compile(ctx)
	if err != nil {
		log.Fatal(err)
	}

	systemPrompt := `## 角色
你是一个短视频剪辑助理。

## 任务
用户已经上传了素材。你需要理解用户需求，并在需要时调用剪辑工具。
重要：每次只调用一个工具，不允许并行工具调用。

【User media upload status】{"Number of media carried in this message sent by the user":0,"Total number of media sent by the user in all conversations":3,"Total number of media in user's media library":{"image number in user's media library":0,"video number in user's media library":3}}`

	userPrompt := "基于我上传的这个素材，做一个 5 秒测试视频。不要联网搜素材，不要配音。现在开始。"

	resp, err := agent.Invoke(ctx, []*schema.Message{
		{Role: schema.System, Content: systemPrompt},
		{Role: schema.User, Content: userPrompt},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Eino agent output messages ===")
	for i, msg := range resp {
		fmt.Printf("\n[%d] role=%s\n", i, msg.Role)
		if msg.Content != "" {
			fmt.Printf("content=%s\n", msg.Content)
		}
		if len(msg.ToolCalls) > 0 {
			b, _ := json.MarshalIndent(msg.ToolCalls, "", "  ")
			fmt.Printf("tool_calls=%s\n", string(b))
		}
	}
}
