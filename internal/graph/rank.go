package graph

// pageRank computes PageRank over a directed graph (src→dst) by power iteration.
// nodes is the full id set; edges maps a source id to its out-neighbours.
// Dangling nodes (no out-edges) redistribute their mass uniformly. This is the
// importance score used to rank the repo map and to order/truncate results.
func pageRank(nodes []int64, edges map[int64][]int64, damping float64, iters int) map[int64]float64 {
	n := len(nodes)
	rank := make(map[int64]float64, n)
	if n == 0 {
		return rank
	}
	init := 1.0 / float64(n)
	for _, id := range nodes {
		rank[id] = init
	}
	teleport := (1 - damping) / float64(n)

	for it := 0; it < iters; it++ {
		// Mass from dangling nodes is shared across all nodes.
		var dangling float64
		for _, id := range nodes {
			if len(edges[id]) == 0 {
				dangling += rank[id]
			}
		}
		base := teleport + damping*dangling/float64(n)
		next := make(map[int64]float64, n)
		for _, id := range nodes {
			next[id] = base
		}
		for src, dsts := range edges {
			if len(dsts) == 0 {
				continue
			}
			share := damping * rank[src] / float64(len(dsts))
			for _, d := range dsts {
				next[d] += share
			}
		}
		rank = next
	}
	return rank
}
