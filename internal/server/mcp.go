package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/GrayCodeAI/yaad/internal/engine"
	gitwatch "github.com/GrayCodeAI/yaad/internal/git"
	"github.com/GrayCodeAI/yaad/internal/graph"
	intentpkg "github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/skill"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/utils"
)

// MCPServer wraps the MCP protocol server for Hawk integration.
type MCPServer struct {
	eng    *engine.Engine
	server *mcpserver.MCPServer
}

// NewMCPServer creates an MCP server with all yaad tools registered.
func NewMCPServer(eng *engine.Engine, _ string) *MCPServer {
	s := &MCPServer{eng: eng}
	s.server = mcpserver.NewMCPServer("yaad", "0.1.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithResourceCapabilities(true, false),
	)
	s.registerTools()
	return s
}

// ServeStdio starts the MCP server on stdin/stdout.
func (s *MCPServer) ServeStdio() error {
	stdio := mcpserver.NewStdioServer(s.server)
	return stdio.Listen(context.Background(), os.Stdin, os.Stdout)
}

func (s *MCPServer) registerTools() {
	add := func(tool mcp.Tool, handler mcpserver.ToolHandlerFunc) {
		s.server.AddTool(tool, handler)
	}

	// yaad_remember
	add(mcp.NewTool("yaad_remember",
		mcp.WithDescription("Store a memory node with type, metadata, and auto-linked entities"),
		mcp.WithString("content", mcp.Required(), mcp.Description("Memory content")),
		mcp.WithString("type", mcp.Description("Node type: convention|decision|bug|spec|task|preference")),
		mcp.WithString("summary", mcp.Description("Short summary")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags")),
		mcp.WithString("project", mcp.Description("Project path")),
		mcp.WithString("scope", mcp.Description("global or project")),
	), s.handleRemember)

	// yaad_recall
	add(mcp.NewTool("yaad_recall",
		mcp.WithDescription("Graph-aware search: BM25 seed → graph expansion → ranked results"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("depth", mcp.Description("Graph expansion depth (default 2)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
		mcp.WithString("type", mcp.Description("Filter by node type")),
		mcp.WithString("project", mcp.Description("Filter by project")),
	), s.handleRecall)

	// yaad_context
	add(mcp.NewTool("yaad_context",
		mcp.WithDescription("Get session-start context: hot-tier subgraph for injection"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleContext)

	// yaad_forget
	add(mcp.NewTool("yaad_forget",
		mcp.WithDescription("Archive a memory node"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Node ID to forget")),
	), s.handleForget)

	// yaad_link
	add(mcp.NewTool("yaad_link",
		mcp.WithDescription("Create an edge between two nodes"),
		mcp.WithString("from_id", mcp.Required(), mcp.Description("Source node ID")),
		mcp.WithString("to_id", mcp.Required(), mcp.Description("Target node ID")),
		mcp.WithString("type", mcp.Required(), mcp.Description("Edge type: caused_by|led_to|supersedes|relates_to|depends_on|touches|learned_in|part_of")),
	), s.handleLink)

	// yaad_unlink
	add(mcp.NewTool("yaad_unlink",
		mcp.WithDescription("Remove an edge"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Edge ID to remove")),
	), s.handleUnlink)

	// yaad_subgraph
	add(mcp.NewTool("yaad_subgraph",
		mcp.WithDescription("Get subgraph around a node via BFS"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Center node ID")),
		mcp.WithNumber("depth", mcp.Description("BFS depth (default 2)")),
	), s.handleSubgraph)

	// yaad_impact
	add(mcp.NewTool("yaad_impact",
		mcp.WithDescription("Impact analysis: what memories are affected if a file changes"),
		mcp.WithString("file", mcp.Required(), mcp.Description("File path")),
		mcp.WithNumber("depth", mcp.Description("Traversal depth (default 3)")),
	), s.handleImpact)

	// yaad_status
	add(mcp.NewTool("yaad_status",
		mcp.WithDescription("Health check: node/edge counts, session count"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleStatus)

	// yaad_task_update
	add(mcp.NewTool("yaad_task_update",
		mcp.WithDescription("Update a task node's content/status"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Task node ID")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Updated task content")),
	), s.handleTaskUpdate)

	// yaad_sessions
	add(mcp.NewTool("yaad_sessions",
		mcp.WithDescription("List recent sessions"),
		mcp.WithString("project", mcp.Description("Project path")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
	), s.handleSessions)

	// yaad_stale
	add(mcp.NewTool("yaad_stale",
		mcp.WithDescription("Show potentially stale memory subgraphs"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleStale)

	// yaad_intent
	add(mcp.NewTool("yaad_intent",
		mcp.WithDescription("Classify query intent (Why/When/Who/How/What/General) for intent-aware retrieval"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Query to classify")),
	), s.handleIntent)

	// yaad_profile
	add(mcp.NewTool("yaad_profile",
		mcp.WithDescription("Get auto-maintained user/project profile: static facts + dynamic recent context"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleProfile)

	// yaad_feedback
	add(mcp.NewTool("yaad_feedback",
		mcp.WithDescription("Approve, edit, or discard a memory node"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Node ID")),
		mcp.WithString("action", mcp.Required(), mcp.Description("approve|edit|discard")),
		mcp.WithString("content", mcp.Description("New content (required for edit)")),
	), s.handleFeedback)

	// yaad_hybrid_recall
	add(mcp.NewTool("yaad_hybrid_recall",
		mcp.WithDescription("Hybrid search: BM25 + vector + graph with RRF fusion ranking"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("depth", mcp.Description("Graph expansion depth (default 2)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
	), s.handleHybridRecall)

	// yaad_proactive
	add(mcp.NewTool("yaad_proactive",
		mcp.WithDescription("Get proactively predicted context for the current session"),
		mcp.WithString("project", mcp.Description("Project path")),
		mcp.WithNumber("budget", mcp.Description("Token budget (default 2000)")),
	), s.handleProactive)

	// yaad_compact
	add(mcp.NewTool("yaad_compact",
		mcp.WithDescription("Compact graph: merge low-confidence memories into summaries"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleCompact)

	// yaad_mental_model
	add(mcp.NewTool("yaad_mental_model",
		mcp.WithDescription("Get auto-evolving project summary: conventions, decisions, architecture"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleMentalModel)

	// yaad_pin
	add(mcp.NewTool("yaad_pin",
		mcp.WithDescription("Pin/unpin a memory node (pinned nodes always appear in context)"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Node ID")),
		mcp.WithBoolean("pinned", mcp.Description("true to pin, false to unpin (default: toggle)")),
	), s.handlePin)

	// yaad_skill_store
	add(mcp.NewTool("yaad_skill_store",
		mcp.WithDescription("Store a procedural skill (step-by-step procedure)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Skill name")),
		mcp.WithString("description", mcp.Required(), mcp.Description("What this skill does")),
		mcp.WithString("steps", mcp.Required(), mcp.Description("JSON array of step descriptions")),
	), s.handleSkillStore)

	// yaad_skill_get
	add(mcp.NewTool("yaad_skill_get",
		mcp.WithDescription("Retrieve a stored skill by name"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Skill name")),
	), s.handleSkillGet)

	// yaad_session_recap
	add(mcp.NewTool("yaad_session_recap",
		mcp.WithDescription("Show what was captured in the last session (pick up where you left off)"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleSessionRecap)
}

// --- Tool handlers ---

func (s *MCPServer) handleRemember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	typ := strArgOr(req, "type", "decision")
	if typ != "" && !engine.IsValidNodeType(typ) {
		return nil, fmt.Errorf("invalid node type: %q", typ)
	}
	in := engine.RememberInput{
		Content: strArg(req, "content"),
		Type:    typ,
		Summary: strArg(req, "summary"),
		Tags:    strArg(req, "tags"),
		Project: strArg(req, "project"),
		Scope:   strArgOr(req, "scope", "project"),
	}
	node, err := s.eng.Remember(ctx, in)
	if err != nil {
		return nil, err
	}
	return jsonResult(node)
}

func (s *MCPServer) handleRecall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	depth := intArgOr(req, "depth", 2)
	if depth <= 0 || depth > 5 {
		depth = 2
	}
	limit := intArgOr(req, "limit", 10)
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	result, err := s.eng.Recall(ctx, engine.RecallOpts{
		Query:   strArg(req, "query"),
		Depth:   depth,
		Limit:   limit,
		Type:    strArg(req, "type"),
		Project: strArg(req, "project"),
	})
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func (s *MCPServer) handleContext(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, err := s.eng.Context(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func (s *MCPServer) handleForget(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.eng.Forget(ctx, strArg(req, "id")); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("forgotten"), nil
}

func (s *MCPServer) handleLink(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	edgeType := strArg(req, "type")
	if edgeType == "" {
		return nil, fmt.Errorf("edge type is required")
	}
	if !graph.IsValidEdgeType(edgeType) {
		return nil, fmt.Errorf("invalid edge type: %q", edgeType)
	}
	edge := &storage.Edge{
		FromID: strArg(req, "from_id"),
		ToID:   strArg(req, "to_id"),
		Type:   edgeType,
		Weight: 1.0,
	}
	edge.ID = fmt.Sprintf("%s-%s-%s", utils.ShortID(edge.FromID), utils.ShortID(edge.ToID), edge.Type)
	if err := s.eng.Graph().AddEdge(ctx, edge); err != nil {
		return nil, err
	}
	return jsonResult(edge)
}

func (s *MCPServer) handleUnlink(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.eng.Graph().RemoveEdge(ctx, strArg(req, "id")); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("unlinked"), nil
}

func (s *MCPServer) handleSubgraph(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	depth := intArgOr(req, "depth", 2)
	if depth <= 0 || depth > 5 {
		depth = 2
	}
	sg, err := s.eng.Graph().ExtractSubgraph(ctx, strArg(req, "id"), depth)
	if err != nil {
		return nil, err
	}
	return jsonResult(sg)
}

func (s *MCPServer) handleImpact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	depth := intArgOr(req, "depth", 3)
	if depth <= 0 || depth > 5 {
		depth = 3
	}
	ids, err := s.eng.Graph().Impact(ctx, strArg(req, "file"), depth)
	if err != nil {
		return nil, err
	}
	// Resolve IDs to nodes
	var nodes []*storage.Node
	for _, id := range ids {
		n, err := s.eng.Store().GetNode(ctx, id)
		if err == nil {
			nodes = append(nodes, n)
		}
	}
	return jsonResult(nodes)
}

func (s *MCPServer) handleStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := s.eng.Status(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return jsonResult(st)
}

func (s *MCPServer) handleTaskUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	node, err := s.eng.Store().GetNode(ctx, strArg(req, "id"))
	if err != nil {
		return nil, err
	}
	if err := s.eng.Store().SaveVersion(ctx, node.ID, node.Content, "agent", "task update"); err != nil {
		return nil, fmt.Errorf("save version: %w", err)
	}
	node.Content = strArg(req, "content")
	node.Version++
	if err := s.eng.Store().UpdateNode(ctx, node); err != nil {
		return nil, err
	}
	return jsonResult(node)
}

func (s *MCPServer) handleSessions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessions, err := s.eng.Store().ListSessions(ctx, strArg(req, "project"), intArgOr(req, "limit", 10))
	if err != nil {
		return nil, err
	}
	return jsonResult(sessions)
}

func (s *MCPServer) handleStale(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project := strArg(req, "project")
	if project == "" {
		project, _ = os.Getwd()
	}
	watcher := gitwatch.New(s.eng.Store(), s.eng.Graph(), project)
	since := time.Now().Add(-7 * 24 * time.Hour)
	reports, err := watcher.StalesSince(ctx, since)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return mcp.NewToolResultText("No stale memories detected in the last 7 days."), nil
	}
	return jsonResult(reports)
}

func (s *MCPServer) handleIntent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := strArg(req, "query")
	i := intentpkg.Classify(query)
	weights := intentpkg.Weights(i)
	return jsonResult(map[string]any{
		"query":   query,
		"intent":  i.String(),
		"weights": weights,
	})
}

func (s *MCPServer) handleProfile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p, err := s.eng.Profile(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"profile":   p,
		"formatted": p.Format(),
	})
}

func (s *MCPServer) handleFeedback(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := strArg(req, "id")
	action := strArg(req, "action")
	content := strArg(req, "content")
	if err := s.eng.Feedback(ctx, id, engine.FeedbackAction(action), content); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("feedback applied: %s on %s", action, utils.ShortID(id))), nil
}

func (s *MCPServer) handleHybridRecall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	hs := engine.NewHybridSearch(s.eng.Store(), s.eng.Graph(), nil)
	scored, err := hs.Search(ctx, strArg(req, "query"), engine.RecallOpts{
		Depth: intArgOr(req, "depth", 2),
		Limit: intArgOr(req, "limit", 10),
	})
	if err != nil {
		return nil, err
	}
	reranked := engine.Rerank(ctx, scored, s.eng.Store())
	return jsonResult(reranked)
}

func (s *MCPServer) handleProactive(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	hs := engine.NewHybridSearch(s.eng.Store(), s.eng.Graph(), nil)
	pc := engine.NewProactiveContext(s.eng, hs)
	budget := intArgOr(req, "budget", 2000)
	nodes, err := pc.Predict(ctx, strArg(req, "project"), budget)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(engine.FormatContext(nodes)), nil
}

func (s *MCPServer) handleCompact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	n, err := s.eng.Compact(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("compacted %d nodes", n)), nil
}

func (s *MCPServer) handleMentalModel(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	model, err := s.eng.MentalModel(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(model.Format()), nil
}

func (s *MCPServer) handlePin(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	node, err := s.eng.Store().GetNode(ctx, strArg(req, "id"))
	if err != nil {
		return nil, err
	}
	if v, ok := req.GetArguments()["pinned"].(bool); ok {
		node.Pinned = v
	} else {
		node.Pinned = !node.Pinned
	}
	if err := s.eng.Store().UpdateNode(ctx, node); err != nil {
		return nil, err
	}
	status := "unpinned"
	if node.Pinned {
		status = "pinned"
	}
	return mcp.NewToolResultText(fmt.Sprintf("%s node %s", status, utils.ShortID(node.ID))), nil
}

func (s *MCPServer) handleSkillStore(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := strArg(req, "name")
	desc := strArg(req, "description")
	stepsJSON := strArg(req, "steps")
	var stepDescs []string
	if err := json.Unmarshal([]byte(stepsJSON), &stepDescs); err != nil {
		return nil, fmt.Errorf("steps must be a JSON array of strings: %w", err)
	}
	steps := make([]skill.Step, len(stepDescs))
	for i, d := range stepDescs {
		steps[i] = skill.Step{Order: i + 1, Description: d}
	}
	sk := &skill.Skill{Name: name, Description: desc, Steps: steps}
	project, _ := os.Getwd()
	node, err := skill.Store(ctx, s.eng, sk, project)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("stored skill %q (id: %s)", name, utils.ShortID(node.ID))), nil
}

func (s *MCPServer) handleSkillGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project, _ := os.Getwd()
	sk, err := skill.Load(ctx, s.eng.Store(), strArg(req, "name"), project)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(skill.Replay(sk)), nil
}

func (s *MCPServer) handleSessionRecap(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project := strArg(req, "project")
	sessions, err := s.eng.Store().ListSessions(ctx, project, 1)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return mcp.NewToolResultText("No previous sessions found."), nil
	}
	last := sessions[0]
	nodes, err := s.eng.Store().ListNodes(ctx, storage.NodeFilter{
		Project:       project,
		SourceSession: last.ID,
	})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("Last session (%s) captured no memories.", last.ID[:8])), nil
	}
	return mcp.NewToolResultText(engine.FormatContext(nodes)), nil
}

// --- helpers ---

func strArg(req mcp.CallToolRequest, key string) string {
	if v, ok := req.GetArguments()[key].(string); ok {
		return v
	}
	return ""
}

func strArgOr(req mcp.CallToolRequest, key, def string) string {
	if v := strArg(req, key); v != "" {
		return v
	}
	return def
}

func intArgOr(req mcp.CallToolRequest, key string, def int) int {
	if v, ok := req.GetArguments()[key].(float64); ok {
		return int(v)
	}
	return def
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}
