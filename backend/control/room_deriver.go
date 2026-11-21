// Package control 是控制层：房间推导这类纯几何逻辑放这里，不碰数据库也不碰 HTTP。
package control

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
)

// Point 是墙的端点。ID 只用来建图，坐标才是几何真相。
type Point struct {
	ID string
	X  float64
	Y  float64
}

// Wall 是无向边，连接两个端点。
type Wall struct {
	A string
	B string
}

// Ring 是推导出来的一个闭合房间，顶点按顺序排列（首尾不重复）。
type Ring struct {
	Points []Point
	Hash   string
	// AreaM2 是房间面积，平方米，保留两位小数（前端直接显示，不再换算）。
	AreaM2 float64
}

// Result 是一次推导的完整结果。
type Result struct {
	Rings []Ring
	// OpenWalls 是没有参与任何闭合区域的墙数：有墙却推不出房间时，
	// 前端据此提示「还没围成闭合区域」，而不是静默显示空白。
	OpenWalls int
}

// Derive 从端点与墙推房间。
//
// 做法是平面图上的半边遍历：每条墙拆成两条有向半边，在每个端点按极角排序；
// 从一条未访问的半边出发，每步都取「最右转」的后继，绕回起点就是一个面。
// 面积符号把外环（无穷面）与内面区分开，只留内面。
//
// 先用「剪枝」去掉悬空墙：一端没有别的墙接续的墙围不出任何东西，
// 留在图里只会让随后绕出的面把这条墙正反走两遍，变成一个假房间。
func Derive(corners []Point, walls []Wall) Result {
	byID := make(map[string]Point, len(corners))
	for _, c := range corners {
		byID[c.ID] = c
	}

	// 只保留两端都存在的墙（脏数据不该让推导崩掉）。
	edges := make([]Wall, 0, len(walls))
	for _, w := range walls {
		if _, ok := byID[w.A]; !ok {
			continue
		}
		if _, ok := byID[w.B]; !ok {
			continue
		}
		if w.A == w.B {
			continue // 自环不是墙
		}
		edges = append(edges, w)
	}

	kept, pruned := pruneDangling(edges)

	rings := deriveFaces(byID, kept)
	return Result{Rings: rings, OpenWalls: pruned}
}

// pruneDangling 反复删掉一端只接一条墙的墙，返回剩下的墙与被删掉的数量。
func pruneDangling(edges []Wall) ([]Wall, int) {
	alive := make([]bool, len(edges))
	for i := range alive {
		alive[i] = true
	}

	for {
		degree := map[string]int{}
		for i, e := range edges {
			if !alive[i] {
				continue
			}
			degree[e.A]++
			degree[e.B]++
		}

		removed := 0
		for i, e := range edges {
			if !alive[i] {
				continue
			}
			if degree[e.A] <= 1 || degree[e.B] <= 1 {
				alive[i] = false
				removed++
			}
		}
		if removed == 0 {
			break
		}
	}

	kept := make([]Wall, 0, len(edges))
	pruned := 0
	for i, e := range edges {
		if alive[i] {
			kept = append(kept, e)
		} else {
			pruned++
		}
	}
	return kept, pruned
}

type halfEdge struct {
	from  string
	to    string
	angle float64
	used  bool
	// next 是「进入该半边终点后继续走的那条半边」，在 buildNext 里算好。
	next int
}

// deriveFaces 返回所有内面（外环按面积符号剔除），并去掉重复面。
func deriveFaces(byID map[string]Point, edges []Wall) []Ring {
	if len(edges) == 0 {
		return nil
	}

	half := make([]halfEdge, 0, len(edges)*2)
	for _, e := range edges {
		half = append(half, newHalfEdge(byID, e.A, e.B))
		half = append(half, newHalfEdge(byID, e.B, e.A))
	}

	// outbound[v] = 从 v 出发的半边下标，按极角升序，供「最右转」查找。
	outbound := map[string][]int{}
	for i := range half {
		outbound[half[i].from] = append(outbound[half[i].from], i)
	}
	for v := range outbound {
		idx := outbound[v]
		sort.Slice(idx, func(a, b int) bool {
			ka, kb := half[idx[a]], half[idx[b]]
			if ka.angle != kb.angle {
				return ka.angle < kb.angle
			}
			// 同角（重合墙理论上进不来，这里只求确定性）
			return ka.to < kb.to
		})
	}

	// 每条半边 i 的后继：先找它的反向半边 rev，再取 rev 在 outbound 里的
	// 前一条（即顺时针方向的下一条）。这条规则保证绕出的面把内部留在同一侧。
	position := map[[2]string]int{}
	for v, idx := range outbound {
		for pos, i := range idx {
			position[[2]string{v, half[i].to}] = pos
		}
	}
	for i := range half {
		v := half[i].to
		revPos, ok := position[[2]string{v, half[i].from}]
		if !ok {
			continue
		}
		idx := outbound[v]
		half[i].next = idx[(revPos-1+len(idx))%len(idx)]
	}

	var rings []Ring
	seen := map[string]bool{}
	for start := range half {
		if half[start].used {
			continue
		}
		var pts []Point
		cur := start
		for {
			if half[cur].used && cur != start {
				break // 走进了别的环（理论上不该发生，防御性处理）
			}
			half[cur].used = true
			pts = append(pts, byID[half[cur].from])
			cur = half[cur].next
			if cur == start {
				break
			}
			if len(pts) > len(half)+1 {
				break
			}
		}
		if cur != start || len(pts) < 3 {
			continue
		}

		signed := signedArea(pts)
		// 「最右转」遍历下，有界内面的有符号面积为正，绕外圈的那个面为负。
		// 只留有界面；外环丢了正是我们想要的（它不是房间）。
		if signed <= 0 {
			continue
		}

		hash := Hash(pts)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		rings = append(rings, Ring{
			Points: pts,
			Hash:   hash,
			AreaM2: math.Round(math.Abs(signed)/2/10000*100) / 100,
		})
	}

	// 面积大的排前面：通常是大房间（客厅）先被点到，层叠顺序也更自然。
	sort.Slice(rings, func(i, j int) bool {
		if rings[i].AreaM2 != rings[j].AreaM2 {
			return rings[i].AreaM2 > rings[j].AreaM2
		}
		return rings[i].Hash < rings[j].Hash
	})
	return rings
}

func newHalfEdge(byID map[string]Point, from, to string) halfEdge {
	a, b := byID[from], byID[to]
	return halfEdge{from: from, to: to, angle: math.Atan2(b.Y-a.Y, b.X-a.X)}
}

func signedArea(pts []Point) float64 {
	sum := 0.0
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		sum += p.X*q.Y - q.X*p.Y
	}
	return sum
}

// Hash 把环规范化后取哈希：起点固定成坐标字典序最小的那个点，方向固定成
// 有符号面积为正（与内面的绕行方向一致），坐标按千分之一厘米取整。
//
// 这样同一间房无论从哪个端点开始画、按什么方向存，算出来的哈希都一样——
// 房间命名就挂在这个哈希上，哈希一漂，名字就丢了。
func Hash(pts []Point) string {
	norm := make([]Point, len(pts))
	copy(norm, pts)
	if signedArea(norm) < 0 {
		for i, j := 0, len(norm)-1; i < j; i, j = i+1, j-1 {
			norm[i], norm[j] = norm[j], norm[i]
		}
	}

	best := 0
	for i := 1; i < len(norm); i++ {
		if less(norm[i], norm[best]) {
			best = i
		}
	}
	norm = append(norm[best:], norm[:best]...)

	var b strings.Builder
	for _, p := range norm {
		fmt.Fprintf(&b, "%.3f,%.3f;", p.X, p.Y)
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(b.String()))
	return fmt.Sprintf("%016x", h.Sum64())
}

func less(a, b Point) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	return a.Y < b.Y
}
