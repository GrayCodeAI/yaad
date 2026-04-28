"""
Yaad Python SDK — persistent memory for coding agents.

Usage:
    from yaad import Yaad

    y = Yaad()
    y.remember(content="Use jose for JWT", type="convention")
    results = y.recall("auth middleware")
    context = y.context()
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Optional
from urllib.request import Request, urlopen
from urllib.error import URLError


@dataclass
class Node:
    id: str = ""
    type: str = ""
    content: str = ""
    summary: str = ""
    scope: str = ""
    project: str = ""
    tier: int = 0
    tags: str = ""
    confidence: float = 0.0
    access_count: int = 0


@dataclass
class Edge:
    id: str = ""
    from_id: str = ""
    to_id: str = ""
    type: str = ""
    weight: float = 0.0


@dataclass
class RecallResult:
    nodes: list[Node] = field(default_factory=list)
    edges: list[Edge] = field(default_factory=list)


class YaadError(Exception):
    pass


class Yaad:
    """Client for the Yaad memory layer REST API."""

    def __init__(self, base_url: str = "http://127.0.0.1:3456"):
        self.base_url = base_url.rstrip("/")

    # --- Core Memory ---

    def remember(
        self,
        content: str,
        type: str = "decision",
        summary: str = "",
        tags: str = "",
        project: str = "",
        scope: str = "project",
        session: str = "",
        agent: str = "",
    ) -> Node:
        """Store a memory node."""
        data = self._post("/yaad/remember", {
            "content": content, "type": type, "summary": summary,
            "tags": tags, "project": project, "scope": scope,
            "session": session, "agent": agent,
        })
        return self._to_node(data)

    def recall(
        self,
        query: str,
        depth: int = 2,
        limit: int = 10,
        type: str = "",
        project: str = "",
    ) -> RecallResult:
        """Graph-aware search: BM25 + vector + graph + temporal."""
        data = self._post("/yaad/recall", {
            "query": query, "depth": depth, "limit": limit,
            "type": type, "project": project,
        })
        return self._to_recall_result(data)

    def context(self, project: str = "") -> RecallResult:
        """Get hot-tier context for session start (~2K tokens)."""
        params = f"?project={project}" if project else ""
        data = self._get(f"/yaad/context{params}")
        return self._to_recall_result(data)

    def forget(self, id: str) -> None:
        """Archive a memory node."""
        self._delete(f"/yaad/forget/{id}")

    # --- Graph ---

    def link(self, from_id: str, to_id: str, type: str) -> Edge:
        """Create an edge between two nodes."""
        data = self._post("/yaad/link", {
            "from_id": from_id, "to_id": to_id, "type": type,
        })
        return Edge(**{k: data.get(k, "") for k in Edge.__dataclass_fields__})

    def subgraph(self, id: str, depth: int = 2) -> RecallResult:
        """Get BFS subgraph around a node."""
        data = self._get(f"/yaad/subgraph/{id}?depth={depth}")
        return self._to_recall_result(data)

    def impact(self, file: str) -> list[Node]:
        """Impact analysis: what memories are affected if this file changes?"""
        data = self._get(f"/yaad/impact/{file}")
        if isinstance(data, list):
            return [self._to_node(n) for n in data]
        return []

    # --- Sessions ---

    def session_start(self, project: str = "", agent: str = "") -> dict:
        """Start a session and get context."""
        return self._post("/yaad/session/start", {"project": project, "agent": agent})

    def session_end(self, id: str, summary: str = "") -> None:
        """End a session."""
        self._post("/yaad/session/end", {"id": id, "summary": summary})

    # --- System ---

    def health(self) -> dict:
        """Health check."""
        return self._get("/yaad/health")

    def stats(self, project: str = "") -> dict:
        """Graph statistics."""
        params = f"?project={project}" if project else ""
        return self._get(f"/yaad/graph/stats{params}")

    # --- Feedback ---

    def approve(self, id: str) -> None:
        self._post("/yaad/feedback", {"id": id, "action": "approve"})

    def edit(self, id: str, new_content: str) -> None:
        self._post("/yaad/feedback", {"id": id, "action": "edit", "new_content": new_content})

    def discard(self, id: str) -> None:
        self._post("/yaad/feedback", {"id": id, "action": "discard"})

    # --- Advanced ---

    def compact(self, project: str = "") -> dict:
        """Compact low-confidence memories."""
        return self._post(f"/yaad/compact?project={project}", {})

    def mental_model(self, project: str = "") -> dict:
        """Get auto-generated project mental model."""
        params = f"?project={project}" if project else ""
        return self._get(f"/yaad/mental-model{params}")

    def intent(self, query: str) -> dict:
        """Classify query intent (Why/When/Who/How/What)."""
        return self._post("/yaad/recall", {"query": query, "limit": 0})

    # --- HTTP helpers ---

    def _get(self, path: str) -> Any:
        try:
            req = Request(f"{self.base_url}{path}")
            with urlopen(req, timeout=10) as resp:
                return json.loads(resp.read())
        except URLError as e:
            raise YaadError(f"GET {path}: {e}") from e

    def _post(self, path: str, body: dict) -> Any:
        try:
            data = json.dumps(body).encode()
            req = Request(f"{self.base_url}{path}", data=data, method="POST")
            req.add_header("Content-Type", "application/json")
            with urlopen(req, timeout=10) as resp:
                return json.loads(resp.read())
        except URLError as e:
            raise YaadError(f"POST {path}: {e}") from e

    def _delete(self, path: str) -> None:
        try:
            req = Request(f"{self.base_url}{path}", method="DELETE")
            urlopen(req, timeout=10)
        except URLError as e:
            raise YaadError(f"DELETE {path}: {e}") from e

    # --- Converters ---

    @staticmethod
    def _to_node(data: dict) -> Node:
        if not data:
            return Node()
        return Node(**{k: data.get(k, Node.__dataclass_fields__[k].default)
                       for k in Node.__dataclass_fields__})

    @staticmethod
    def _to_recall_result(data: dict) -> RecallResult:
        if not data:
            return RecallResult()
        nodes = [Yaad._to_node(n) for n in (data.get("nodes") or [])]
        edges = [Edge(**{k: e.get(k, "") for k in Edge.__dataclass_fields__})
                 for e in (data.get("edges") or [])]
        return RecallResult(nodes=nodes, edges=edges)
