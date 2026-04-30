// Package server provides the gRPC server for yaad.
// Uses google.golang.org/grpc with a hand-written service descriptor
// (no protoc required — messages are JSON-encoded over gRPC).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/version"
)

// --- gRPC message types (JSON-serializable) ---

type GRPCNode struct {
	ID            string  `json:"id"`
	Type          string  `json:"type"`
	Content       string  `json:"content"`
	Summary       string  `json:"summary"`
	Scope         string  `json:"scope"`
	Project       string  `json:"project"`
	Tier          int     `json:"tier"`
	Tags          string  `json:"tags"`
	Confidence    float64 `json:"confidence"`
	SourceSession string  `json:"source_session"`
	SourceAgent   string  `json:"source_agent"`
}

type GRPCEdge struct {
	ID     string  `json:"id"`
	FromID string  `json:"from_id"`
	ToID   string  `json:"to_id"`
	Type   string  `json:"type"`
	Acyclic bool   `json:"acyclic"`
	Weight float64 `json:"weight"`
}

type MemoryEvent struct {
	Event string    `json:"event"` // created|updated|deleted
	Node  *GRPCNode `json:"node"`
}

// --- GRPCServer ---

// GRPCServer implements the yaad gRPC service.
type GRPCServer struct {
	eng      *engine.Engine
	addr     string
	mu       sync.RWMutex
	watchers []chan *MemoryEvent
	srv      *grpc.Server
}

// NewGRPCServer creates a gRPC server.
func NewGRPCServer(eng *engine.Engine, addr string) *GRPCServer {
	return &GRPCServer{eng: eng, addr: addr}
}

// Shutdown gracefully stops the gRPC server.
func (s *GRPCServer) Shutdown() {
	if s.srv != nil {
		s.srv.GracefulStop()
	}
}

// ListenAndServe starts the gRPC server.
func (s *GRPCServer) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.srv = grpc.NewServer()
	// Register as an untyped service using grpc.ServiceDesc
	s.srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "yaad.Yaad",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Remember",     Handler: s.grpcRemember},
			{MethodName: "Recall",       Handler: s.grpcRecall},
			{MethodName: "Context",      Handler: s.grpcContext},
			{MethodName: "Forget",       Handler: s.grpcForget},
			{MethodName: "Link",         Handler: s.grpcLink},
			{MethodName: "Unlink",       Handler: s.grpcUnlink},
			{MethodName: "Subgraph",     Handler: s.grpcSubgraph},
			{MethodName: "Impact",       Handler: s.grpcImpact},
			{MethodName: "SessionStart", Handler: s.grpcSessionStart},
			{MethodName: "SessionEnd",   Handler: s.grpcSessionEnd},
			{MethodName: "Health",       Handler: s.grpcHealth},
			{MethodName: "GraphStats",   Handler: s.grpcGraphStats},
		},
		Streams: []grpc.StreamDesc{
			{StreamName: "WatchMemories", Handler: s.grpcWatchMemories, ServerStreams: true},
		},
	}, s)
	fmt.Printf("yaad gRPC listening on %s\n", s.addr)
	return s.srv.Serve(ln)
}

// NotifyWatchers broadcasts a memory event to all active WatchMemories streams.
func (s *GRPCServer) NotifyWatchers(event string, node *storage.Node) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ev := &MemoryEvent{Event: event, Node: storageToGRPC(node)}
	for _, ch := range s.watchers {
		select {
		case ch <- ev:
		default: // drop if slow consumer
		}
	}
}

// --- Unary handlers ---

func (s *GRPCServer) grpcRemember(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var in engine.RememberInput
	if err := dec(&in); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if in.Type != "" && !engine.IsValidNodeType(in.Type) {
		return nil, status.Errorf(codes.InvalidArgument, "invalid node type: %q", in.Type)
	}
	node, err := s.eng.Remember(ctx, in)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	s.NotifyWatchers("created", node)
	return storageToGRPC(node), nil
}

func (s *GRPCServer) grpcRecall(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var opts engine.RecallOpts
	if err := dec(&opts); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	result, err := s.eng.Recall(ctx, opts)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return recallToGRPC(result), nil
}

func (s *GRPCServer) grpcContext(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct{ Project string `json:"project"` }
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	result, err := s.eng.Context(ctx, req.Project)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return recallToGRPC(result), nil
}

func (s *GRPCServer) grpcForget(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct{ ID string `json:"id"` }
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if err := s.eng.Forget(ctx, req.ID); err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return map[string]string{}, nil
}

func (s *GRPCServer) grpcLink(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
		Type   string `json:"type"`
	}
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if req.Type == "" {
		return nil, status.Errorf(codes.InvalidArgument, "edge type is required")
	}
	if !graph.IsValidEdgeType(req.Type) {
		return nil, status.Errorf(codes.InvalidArgument, "invalid edge type: %q", req.Type)
	}
	edge := &storage.Edge{
		ID: uuid.New().String(), FromID: req.FromID, ToID: req.ToID, Type: req.Type, Weight: 1.0,
	}
	if err := s.eng.Graph().AddEdge(ctx, edge); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return GRPCEdge{ID: edge.ID, FromID: edge.FromID, ToID: edge.ToID, Type: edge.Type, Acyclic: edge.Acyclic, Weight: edge.Weight}, nil
}

func (s *GRPCServer) grpcUnlink(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct{ ID string `json:"id"` }
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if err := s.eng.Graph().RemoveEdge(ctx, req.ID); err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return map[string]string{}, nil
}

func (s *GRPCServer) grpcSubgraph(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct {
		ID    string `json:"id"`
		Depth int    `json:"depth"`
	}
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if req.Depth == 0 {
		req.Depth = 2
	}
	if req.Depth > maxGraphDepth {
		return nil, status.Errorf(codes.InvalidArgument, "depth exceeds maximum of %d", maxGraphDepth)
	}
	sg, err := s.eng.Graph().ExtractSubgraph(ctx, req.ID, req.Depth)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return subgraphToGRPC(sg), nil
}

func (s *GRPCServer) grpcImpact(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct {
		File  string `json:"file"`
		Depth int    `json:"depth"`
	}
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if req.Depth == 0 {
		req.Depth = 3
	}
	if req.Depth > maxGraphDepth {
		return nil, status.Errorf(codes.InvalidArgument, "depth exceeds maximum of %d", maxGraphDepth)
	}
	ids, err := s.eng.Graph().Impact(ctx, req.File, req.Depth)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	var nodes []*GRPCNode
	for _, id := range ids {
		if n, err := s.eng.Store().GetNode(ctx, id); err == nil {
			nodes = append(nodes, storageToGRPC(n))
		}
	}
	return map[string]interface{}{"nodes": nodes}, nil
}

func (s *GRPCServer) grpcSessionStart(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct {
		Project string `json:"project"`
		Agent   string `json:"agent"`
	}
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	sessID, err := s.eng.StartSession(ctx, req.Project, req.Agent)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create session: %v", err)
	}
	result, err := s.eng.Context(ctx, req.Project)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	resp := recallToGRPC(result)
	resp["session_id"] = sessID
	return resp, nil
}

func (s *GRPCServer) grpcSessionEnd(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	var req struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	}
	if err := dec(&req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if err := s.eng.Store().EndSession(ctx, req.ID, req.Summary); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return map[string]string{"id": req.ID, "summary": req.Summary}, nil
}

func (s *GRPCServer) grpcHealth(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	return map[string]string{"status": "ok", "version": version.String()}, nil
}

func (s *GRPCServer) grpcGraphStats(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	st, err := s.eng.Status(ctx, "")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return st, nil
}

// --- Streaming handler ---

func (s *GRPCServer) grpcWatchMemories(srv interface{}, stream grpc.ServerStream) error {
	ch := make(chan *MemoryEvent, 32)
	s.mu.Lock()
	s.watchers = append(s.watchers, ch)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		for i, w := range s.watchers {
			if w == ch {
				s.watchers = append(s.watchers[:i], s.watchers[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}()

	for {
		select {
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			if err := stream.SendMsg(b); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// --- Conversion helpers ---

func storageToGRPC(n *storage.Node) *GRPCNode {
	if n == nil {
		return nil
	}
	return &GRPCNode{
		ID: n.ID, Type: n.Type, Content: n.Content, Summary: n.Summary,
		Scope: n.Scope, Project: n.Project, Tier: n.Tier, Tags: n.Tags,
		Confidence: n.Confidence, SourceSession: n.SourceSession, SourceAgent: n.SourceAgent,
	}
}

func recallToGRPC(r *engine.RecallResult) map[string]interface{} {
	nodes := make([]*GRPCNode, 0, len(r.Nodes))
	for _, n := range r.Nodes {
		nodes = append(nodes, storageToGRPC(n))
	}
	edges := make([]GRPCEdge, 0, len(r.Edges))
	for _, e := range r.Edges {
		edges = append(edges, GRPCEdge{ID: e.ID, FromID: e.FromID, ToID: e.ToID, Type: e.Type, Acyclic: e.Acyclic, Weight: e.Weight})
	}
	return map[string]interface{}{"nodes": nodes, "edges": edges}
}

func subgraphToGRPC(sg *graph.Subgraph) map[string]interface{} {
	nodes := make([]*GRPCNode, 0, len(sg.Nodes))
	for _, n := range sg.Nodes {
		nodes = append(nodes, storageToGRPC(n))
	}
	edges := make([]GRPCEdge, 0, len(sg.Edges))
	for _, e := range sg.Edges {
		edges = append(edges, GRPCEdge{ID: e.ID, FromID: e.FromID, ToID: e.ToID, Type: e.Type, Acyclic: e.Acyclic, Weight: e.Weight})
	}
	return map[string]interface{}{"nodes": nodes, "edges": edges}
}
