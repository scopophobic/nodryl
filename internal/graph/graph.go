package graph

type Evidence string

const (
	Static   Evidence = "code"
	Observed Evidence = "runtime"
)

type Node struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Language string   `json:"language,omitempty"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Evidence Evidence `json:"evidence"`
}

type Edge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Kind     string   `json:"kind"`
	Evidence Evidence `json:"evidence"`
}

type Graph struct {
	Root      string   `json:"root"`
	Nodes     []Node   `json:"nodes"`
	Edges     []Edge   `json:"edges"`
	Warnings  []string `json:"warnings,omitempty"`
	nodeIndex map[string]int
	edgeIndex map[Edge]struct{}
}

func (g *Graph) AddNode(n Node) {
	if g.nodeIndex == nil {
		g.nodeIndex = make(map[string]int, len(g.Nodes))
		for i, existing := range g.Nodes {
			g.nodeIndex[existing.ID] = i
		}
	}
	if _, exists := g.nodeIndex[n.ID]; exists {
		return
	}
	g.nodeIndex[n.ID] = len(g.Nodes)
	g.Nodes = append(g.Nodes, n)
}

func (g *Graph) AddEdge(e Edge) {
	if g.edgeIndex == nil {
		g.edgeIndex = make(map[Edge]struct{}, len(g.Edges))
		for _, existing := range g.Edges {
			g.edgeIndex[existing] = struct{}{}
		}
	}
	if _, exists := g.edgeIndex[e]; exists {
		return
	}
	g.edgeIndex[e] = struct{}{}
	g.Edges = append(g.Edges, e)
}

func (g Graph) NodeByID(id string) (Node, bool) {
	if index, ok := g.nodeIndex[id]; ok && index < len(g.Nodes) {
		return g.Nodes[index], true
	}
	for _, node := range g.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return Node{}, false
}

func (g Graph) Outgoing(id string) []Edge {
	out := []Edge{}
	for _, edge := range g.Edges {
		if edge.From == id {
			out = append(out, edge)
		}
	}
	return out
}

func (g Graph) Incoming(id string) []Edge {
	out := []Edge{}
	for _, edge := range g.Edges {
		if edge.To == id {
			out = append(out, edge)
		}
	}
	return out
}

func (g Graph) Targets(from, kind string) []Node {
	out := []Node{}
	for _, edge := range g.Edges {
		if edge.From != from || edge.Kind != kind {
			continue
		}
		for _, node := range g.Nodes {
			if node.ID == edge.To {
				out = append(out, node)
				break
			}
		}
	}
	return out
}
