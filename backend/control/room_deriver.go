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
// 从一条未访问的半边出发，每步取反向半边的顺时针前驱（内部恒在左侧），
// 绕回起点就是一个面。
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
	valid := make([]Wall, 0, len(walls))
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
		valid = append(valid, w)
	}

	// 平面化：一面墙的中间可能落着别的墙的端点（用户画内墙时接在外墙上）。
	// 这类端点不切分就与那面墙没有拓扑关系，内墙等于悬空，房间推不出来。
	// 切分只发生在这里——库里的墙、前端的墙、墙的 id 都不变。
	edges := nodeEdges(byID, valid)
	kept := pruneDangling(edges)

	rings, used := deriveFaces(byID, kept)

	// 一面用户画的墙算不算「围不上」，看它切出来的任意一段有没有进环；
	// 一段都没进，说明这面墙整体是悬空的（或只是连接两处的桥）。
	usedOrigin := make([]bool, len(valid))
	for pair := range used {
		if o, ok := originOfEdge(kept, pair); ok {
			usedOrigin[o] = true
		}
	}
	open := 0
	for _, ok := range usedOrigin {
		if !ok {
			open++
		}
	}
	return Result{Rings: rings, OpenWalls: open}
}

// edge 是平面化之后的一条线段，origin 指向它来自哪一面用户画的墙。
type edge struct {
	A      string
	B      string
	origin int
}

// nodeEdges 把落在墙内部的端点插进去，把一面墙切成若干段。
// 端点落在墙的延长线上但不在墙内（t∉(0,1)）不算接上。
func nodeEdges(byID map[string]Point, walls []Wall) []edge {
	out := make([]edge, 0, len(walls))
	type hit struct {
		t  float64
		id string
	}
	for i, w := range walls {
		a, b := byID[w.A], byID[w.B]
		hits := make([]hit, 0, 4)
		for id, c := range byID {
			if id == w.A || id == w.B {
				continue
			}
			if t, ok := projectOnSegment(a, b, c); ok {
				hits = append(hits, hit{t: t, id: id})
			}
		}
		sort.Slice(hits, func(x, y int) bool {
			if hits[x].t != hits[y].t {
				return hits[x].t < hits[y].t
			}
			return hits[x].id < hits[y].id
		})

		prev := w.A
		for _, h := range hits {
			if h.id == prev {
				continue
			}
			out = append(out, edge{A: prev, B: h.id, origin: i})
			prev = h.id
		}
		out = append(out, edge{A: prev, B: w.B, origin: i})
	}
	return out
}

// projectOnSegment 判断 c 是否落在 a→b 的内部，返回参数 t。
// 容差用 1e-6 厘米：坐标由前端按 1 cm 取整，这个量级不会误判。
func projectOnSegment(a, b, c Point) (t float64, ok bool) {
	dx, dy := b.X-a.X, b.Y-a.Y
	len2 := dx*dx + dy*dy
	if len2 == 0 {
		return 0, false
	}
	t = ((c.X-a.X)*dx + (c.Y-a.Y)*dy) / len2
	if t <= 1e-6 || t >= 1-1e-6 {
		return 0, false
	}
	// 点到直线的垂距
	cross := dx*(c.Y-a.Y) - dy*(c.X-a.X)
	if math.Abs(cross)/math.Sqrt(len2) > 1e-6 {
		return 0, false
	}
	return t, true
}

func originOfEdge(edges []edge, pair [2]string) (int, bool) {
	for _, e := range edges {
		if keyPair(e.A, e.B) == pair {
			return e.origin, true
		}
	}
	return 0, false
}

// pruneDangling 反复删掉一端只接一条墙的墙，返回剩下的墙与被删掉的数量。
func pruneDangling(edges []edge) []edge {
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

	kept := make([]edge, 0, len(edges))
	for i, e := range edges {
		if alive[i] {
			kept = append(kept, e)
		}
	}
	return kept
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
func deriveFaces(byID map[string]Point, edges []edge) ([]Ring, map[[2]string]bool) {
	used := map[[2]string]bool{}
	if len(edges) == 0 {
		return nil, used
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

	// 每条半边 i 的后继：先找它的反向半边 rev，再取 rev 在 outbound（按极角升序）
	// 里的前一条——也就是顺时针方向的下一条。这条规则保证绕出的面把内部留在左侧，
	// 于是有界内面的有符号面积为正（见下方 deriveFaces 的筛环）。
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
		pairs := make([][2]string, 0, 8)
		cur := start
		for {
			if half[cur].used && cur != start {
				break // 走进了别的环（理论上不该发生，防御性处理）
			}
			half[cur].used = true
			pts = append(pts, byID[half[cur].from])
			pairs = append(pairs, keyPair(half[cur].from, half[cur].to))
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
		// 沿「内部在左」的方向绕行时，有界内面的有符号面积为正，绕外圈的那个面为负。
		// 只留有界面；外环丢了正是我们想要的（它不是房间）。
		if signed <= 0 {
			continue
		}

		// 折掉共线的中间点：平面化会在直墙里插入接点，如果照原样参与哈希，
		// 同一间房的键会随「别人在旁边接了一面墙」而漂移，房间名字就丢了。
		pts = collapseCollinear(pts)
		hash := Hash(pts)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		for _, pair := range pairs {
			used[pair] = true
		}
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
	return rings, used
}

// collapseCollinear 去掉位于两邻居连线上的中间点。
// 顺序保持，点数不足 3 时原样返回（退化图形交给上层筛掉）。
func collapseCollinear(pts []Point) []Point {
	if len(pts) < 3 {
		return pts
	}
	keep := make([]Point, 0, len(pts))
	n := len(pts)
	for i := 0; i < n; i++ {
		prev := pts[(i-1+n)%n]
		cur := pts[i]
		next := pts[(i+1)%n]
		ax, ay := cur.X-prev.X, cur.Y-prev.Y
		bx, by := next.X-cur.X, next.Y-cur.Y
		if math.Abs(ax*by-ay*bx) < 1e-6 && ax*bx+ay*by >= 0 {
			continue // 共线且同向，中间点是多余的
		}
		keep = append(keep, cur)
	}
	if len(keep) < 3 {
		return pts
	}
	return keep
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
	norm := collapseCollinear(pts)
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

// unusedEdges 数出没有落在任何一个环边上的墙。
func unusedEdges(edges []Wall, rings []Ring) int {
	used := make(map[[2]string]bool, len(edges)*2)
	for _, ring := range rings {
		for i, p := range ring.Points {
			q := ring.Points[(i+1)%len(ring.Points)]
			used[keyPair(p.ID, q.ID)] = true
		}
	}
	n := 0
	for _, e := range edges {
		if !used[keyPair(e.A, e.B)] {
			n++
		}
	}
	return n
}

func keyPair(a, b string) [2]string {
	if a < b {
		return [2]string{a, b}
	}
	return [2]string{b, a}
}

// ArcPolyline 把一面弧墙离散成折线顶点（不含两端），用于房间推导。
//
// 参数是两端坐标与弧度（度，正负表示往弦的哪一侧弯）。按 ≤5° 一段采样，
// 90° 弧是 18 段，面积误差 < 0.1%。弧本身不落库：半径与圆心由弦长与弧角反解。
func ArcPolyline(a, b Point, extentDeg float64) []Point {
	theta := extentDeg * math.Pi / 180
	if math.Abs(theta) < 1e-6 {
		return nil
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	d := math.Hypot(dx, dy)
	if d < 1e-9 {
		return nil
	}
	// 圆心在弦的中垂线上：h = (d/2) / tan(θ/2)，正 θ 时落在弦的左侧。
	h := (d / 2) / math.Tan(theta/2)
	mx, my := (a.X+b.X)/2, (a.Y+b.Y)/2
	nx, ny := -dy/d, dx/d
	cx, cy := mx+nx*h, my+ny*h
	r := math.Hypot(a.X-cx, a.Y-cy)
	start := math.Atan2(a.Y-cy, a.X-cx)
	end := math.Atan2(b.Y-cy, b.X-cx)
	delta := end - start
	for delta > math.Pi {
		delta -= 2 * math.Pi
	}
	for delta < -math.Pi {
		delta += 2 * math.Pi
	}
	// 采样步数：按 |δ| 每 5° 一段，至少 2 段。
	steps := int(math.Ceil(math.Abs(delta) / (5 * math.Pi / 180)))
	if steps < 2 {
		steps = 2
	}
	out := make([]Point, 0, steps-1)
	for i := 1; i < steps; i++ {
		ang := start + delta*float64(i)/float64(steps)
		out = append(out, Point{X: cx + r*math.Cos(ang), Y: cy + r*math.Sin(ang)})
	}
	return out
}
