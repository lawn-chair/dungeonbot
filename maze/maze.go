package cwmaze

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"strings"
	"unicode"

	pqueue "github.com/nu7hatch/gopqueue"
)

const (
	tWALL     = 0
	tPATH     = 1
	tFAMOUS   = 2
	tFOUNTAIN = 3
	tCHEST    = 4
	tBONFIRE  = 5
	tMONSTER  = 6
	tBOSS     = 7
	tTEST     = 10
)

// Represents a Maze, call Load with an image to initialize
type Maze struct {
	Pixels    [][]uint8     `json:"pixels"`
	Types     map[uint8]int `json:"types"`
	Boss      point         `json:"boss"`
	Chests    []point       `json:"chests"`
	Fountains []point       `json:"fountains"`
}

func detectPixelType(img image.Image, rect image.Rectangle) uint8 {
	red, green, blue := 0.0, 0.0, 0.0
	for y := rect.Bounds().Min.Y; y < rect.Bounds().Max.Y; y++ {
		for x := rect.Bounds().Min.X; x < rect.Bounds().Max.X; x++ {
			c := img.At(x, y)
			col := color.RGBAModel.Convert(c).(color.RGBA)
			red += float64(col.R)
			green += float64(col.G)
			blue += float64(col.B)
			//fmt.Println(x, y, red, green, blue)
		}
	}

	rectArea := float64(rect.Dx() * rect.Dy())
	//fmt.Println(rectArea)

	red = math.Floor(red / rectArea)
	green = math.Floor(green / rectArea)
	blue = math.Floor(blue / rectArea)
	if red < 50 && green < 50 && blue < 50 {
		return tWALL
	} else if red > 200 && green > 200 && blue > 200 {
		return tPATH
	} else if red > 200 && green > 150 {
		return tBONFIRE
	} else if red > 200 {
		return tBOSS
	} else if green > 200 && blue > 100 {
		return tCHEST
	} else if green > 160 {
		return tFOUNTAIN
	} else if blue > 200 {
		if red > 100 && green > 100 {
			return tMONSTER
		} else {
			return tFAMOUS
		}
	} else {
		fmt.Println("Unknown color combination: ", red, green, blue)
		return 9
	}
}

// Initialize a Maze, image should come from Chat Wars
func (m *Maze) Load(img image.Image) {
	m.Types = make(map[uint8]int)
	m.Pixels = make([][]uint8, img.Bounds().Max.Y/5)
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y += 5 {
		m.Pixels[y/5] = make([]uint8, img.Bounds().Max.X/5)
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x += 5 {
			p := detectPixelType(img, image.Rect(x+1, y+1, x+4, y+4))
			here := point{x / 5, y / 5}
			switch p {
			case tBOSS:
				m.Boss = here
			case tCHEST:
				m.Chests = append(m.Chests, here)
			case tFOUNTAIN:
				m.Fountains = append(m.Fountains, here)
			}
			m.Pixels[y/5][x/5] = p
			m.Types[p]++
		}
	}
}

func (m Maze) SearchByScribble(scribble string) Scribble {
	state := parseScribble(scribble)

	for y := range m.Pixels {
		if y+len(state.Points) > len(m.Pixels) {
			break
		}
		for x := range m.Pixels[y] {
			if x+len(state.Points[0]) > len(m.Pixels[y]) {
				break
			}

			// start with match = true, bail if we find a mismatch
			match := true
			for sy := range state.Points {
				//fmt.Printf("Checking {%d, %d}\n", x, y)
				for sx := range state.Points[sy] {
					//fmt.Printf("{%d, %d} %d == %d\n", x+sx, y+sy, m.Pixels[y+sy][x+sx], state.scribble[sy][sx])
					if state.Points[sy][sx] != 255 &&
						state.Points[sy][sx] != m.Pixels[y+sy][x+sx] {

						// player location matches anything but wall
						// fountain and chest look the same in a scribble
						if !((state.Points[sy][sx] == 254 && m.Pixels[y+sy][x+sx] != tWALL) ||
							(state.Points[sy][sx] == tFOUNTAIN && m.Pixels[y+sy][x+sx] == tCHEST)) {
							match = false
							break
						}

					}
				}
			}

			if match {
				state.Matches = append(state.Matches, point{x, y})
			}
		}
	}

	return state
}

func (m Maze) neighbors(p point) (ret []point) {
	possible := []point{
		{p.X - 1, p.Y},
		{p.X + 1, p.Y},
		{p.X, p.Y - 1},
		{p.X, p.Y + 1},
	}

	for _, p := range possible {
		if m.Pixels[p.Y][p.X] != tWALL {
			ret = append(ret, p)
		}
	}
	return
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func heuristic(a, b point) int {
	return abs(a.X-b.X) + abs(a.Y-b.Y)
}

func travelCost(p uint8) int {
	switch p {
	case tCHEST:
		return 1
	case tMONSTER:
		return 10
	case tFOUNTAIN:
		return 1
	default:
		return 5
	}
}

func (m Maze) searchPathAStar(start, end point) []point {
	startItem := Item{
		start,
		0,
	}

	frontier := pqueue.New(0)

	frontier.Enqueue(&startItem)

	came_from := make(map[point]point)
	cost_so_far := make(map[point]int)

	came_from[startItem.p] = startItem.p
	cost_so_far[startItem.p] = 0

	for frontier.Len() > 0 {
		current := frontier.Dequeue().(*Item)
		if current.p == end {
			break
		}
		//fmt.Println("At: ", current.p)
		for _, next := range m.neighbors(current.p) {
			//fmt.Println("Checking neighbor: ", next)
			//fmt.Println("Travel Cost: ", travelCost(m.Pixels[next.Y][next.X]))
			new_cost := cost_so_far[current.p] + travelCost(m.Pixels[next.Y][next.X])
			_, exists := cost_so_far[next]

			//fmt.Println("Cost so far, ", cost_so_far[next], " new_cost, ", new_cost)
			if !exists || new_cost < cost_so_far[next] {
				cost_so_far[next] = new_cost
				priority := new_cost + heuristic(next, end)
				frontier.Enqueue(&Item{p: next, priority: priority})
				came_from[next] = current.p
			}
		}
	}

	if _, exists := came_from[end]; exists {
		//fmt.Println("Found path")
		path := []point{end}
		for last := came_from[end]; last != startItem.p; last = came_from[last] {
			path = append(path, last)
		}
		path = append(path, startItem.p)
		return path
	} else {
		return make([]point, 0)
	}
}

type fullPath struct {
	a []point
	b []point
}

func filter[T any](ss []T, test func(T) bool) (ret []T) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}

type searchState struct {
	end     point
	path    []point
	visited map[point]struct{}
}

func (m Maze) searchPathWithSteps(start, end point, steps int) (final []point) {
	if len(m.Fountains) == 0 {
		return make([]point, 0)
	}
	list := make(itemList, len(m.Fountains))
	//counter := 0
	stack := make([]searchState, 1)

	stack[0] = searchState{end, make([]point, 0), make(map[point]struct{})}

	solutions := make([][]point, 0)
	for len(stack) > 0 {
		//fmt.Println("Stack length: ", len(stack))
		state := stack[len(stack)-1]
		stack = stack[0 : len(stack)-1]

		for i := range m.Fountains {
			list[i] = itemDistance{
				m.Fountains[i],
				heuristic(state.end, m.Fountains[i]),
			}
		}
		sort.Sort(list)

		filteredList := filter(list, func(item itemDistance) bool { _, visited := state.visited[item.location]; return !visited })
		filteredList = filter(filteredList, func(item itemDistance) bool { return item.distance > 8 })

		//fmt.Println(filteredList)

		const PATHS_TO_TRY = 8
		paths := make([]fullPath, PATHS_TO_TRY)

		for i := 0; i < PATHS_TO_TRY; i++ {
			paths[i].a = m.searchPathAStar(state.end, filteredList[i].location)
			paths[i].b = m.searchPathAStar(start, filteredList[i].location)
		}

		//lowestTotal := 10000
		//var best fullPath
		for i := 0; i < PATHS_TO_TRY; i++ {
			//fmt.Println("Path from: ", state.end, " to: ", filteredList[i].location)
			//fmt.Println("fountain: ", filteredList[i].location, "len a: ", len(paths[i].a), " len b: ", len(paths[i].b))
			if len(paths[i].a) > steps || len(paths[i].a) == 0 || len(paths[i].b) == 0 {
				continue
			}

			if len(paths[i].b) <= steps {
				fmt.Println("winner")
				solution := make([]point, 0, len(state.path)+len(paths[i].a)+len(paths[i].b))
				solution = append(solution, state.path...)
				solution = append(solution, paths[i].a...)
				solution = append(solution, paths[i].b...)
				solutions = append(solutions, solution)
				/*
					final = append(final, state.path...)
					final = append(final, paths[i].a...)
					final = append(final, paths[i].b...)
					return
				*/
			} else {
				state.visited[filteredList[i].location] = struct{}{}
				//fmt.Println("qualifying path")
				//if len(paths[i].a)+len(paths[i].b) < lowestTotal {
				//lowestTotal = len(paths[i].a) + len(paths[i].b)
				//best = paths[i]
				//fmt.Println(best.a[0], filteredList[i])]
				tmp := make([]point, 0, len(state.path)+len(paths[i].a))
				tmp = append(tmp, state.path...)
				tmp = append(tmp, paths[i].a...)
				stack = append(stack, searchState{
					filteredList[i].location,
					tmp,
					state.visited})

			}

			//}

		}

		/*
			fmt.Println(best.a)
			if len(best.a) == 0 {
				return
				//return make([]point, 0)
			}
			final = append(final, best.a...)
			end = best.a[len(best.a)-1]
			counter += 1
			if counter > 50 {
				fmt.Println("counter expired")
				final = append(final, best.b...)
				return
				//return make([]point, 0)
			}
		*/
	}
	fmt.Println("Solutions: ", len(solutions))
	if len(solutions) >= 1 {
		var bestPath []point
		shortest := 1000
		for _, path := range solutions {
			fmt.Println(len(path))
			if len(path) < shortest {
				shortest = len(path)
				bestPath = path
			}
		}
		return bestPath
	}

	return make([]point, 0)
}

func (m Maze) FindPathToBossFrom(s *Scribble) ([]point, error) {
	value := m.searchPathWithSteps(
		point{s.Matches[0].X + s.PlayerLocation.X, s.Matches[0].Y + s.PlayerLocation.Y},
		m.Boss, 35)
	if len(value) == 0 {
		value = m.searchPathAStar(
			point{s.Matches[0].X + s.PlayerLocation.X, s.Matches[0].Y + s.PlayerLocation.Y},
			m.Boss)
		return value, errors.New("no path found when counting steps, providing shortest path")
	}

	return value, nil
}

type itemDistance struct {
	location point
	distance int
}

type itemList []itemDistance

func (c itemList) Len() int           { return len(c) }
func (c itemList) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c itemList) Less(i, j int) bool { return c[i].distance < c[j].distance }

func (m Maze) FindPathToChestFrom(s *Scribble) []point {
	player := point{s.Matches[0].X + s.PlayerLocation.X, s.Matches[0].Y + s.PlayerLocation.Y}

	list := make(itemList, len(m.Chests))

	for c := range m.Chests {
		list[c] = itemDistance{
			m.Chests[c],
			heuristic(player, m.Chests[c]),
		}
	}

	sort.Sort(list)
	fmt.Println(list)
	return m.searchPathAStar(player, list[0].location)

}

func (m Maze) FindPathTo(what string, s *Scribble) ([]point, error) {
	switch what {
	case "boss":
		return m.FindPathToBossFrom(s)
	case "chest":
		return m.FindPathToChestFrom(s), nil
	case "shortest":
		player := point{s.Matches[0].X + s.PlayerLocation.X, s.Matches[0].Y + s.PlayerLocation.Y}
		return m.searchPathAStar(player, m.Boss), nil
	default:
		return make([]point, 0), nil
	}
}

func (m Maze) ColorModel() color.Model {
	return color.RGBAModel
}

func (m Maze) Bounds() image.Rectangle {
	max := len(m.Pixels) * 5
	return image.Rect(0, 0, max, max)
}

func (m Maze) At(x, y int) color.Color {
	return mazeColorMap(m.Pixels[y/5][x/5])
}

func (m Maze) String() string {
	return fmt.Sprintf("This map has %d chests, %d fountains and %d monsters. Boss located at {%d, %d}.", m.Types[tCHEST], m.Types[tFOUNTAIN], m.Types[tMONSTER], m.Boss.X, m.Boss.Y)
}

func mazeColorMap(val uint8) color.Color {
	switch val {
	case tWALL:
		return color.RGBA{0, 0, 0, 255}
	case tPATH:
		return color.RGBA{255, 255, 255, 255}
	case tBONFIRE:
		return color.RGBA{255, 165, 0, 255}
	case tCHEST:
		return color.RGBA{0, 255, 255, 255}
	case tBOSS:
		return color.RGBA{255, 0, 0, 255}
	case tFOUNTAIN:
		return color.RGBA{30, 180, 30, 255}
	case tMONSTER:
		return color.RGBA{140, 120, 255, 255}
	case tFAMOUS:
		return color.RGBA{50, 50, 255, 255}
	default:
		return color.RGBA{251, 4, 253, 255}
	}
}

type point struct {
	X int
	Y int
}

type Scribble struct {
	Points         [][]uint8 `json:"points"`
	PlayerLocation point     `json:"playerLocation"`
	Matches        []point   `json:"matches"`
}

func (s Scribble) ColorModel() color.Model {
	return color.RGBAModel
}

func (s Scribble) Bounds() image.Rectangle {
	ymax := len(s.Points) * 5
	xmax := len(s.Points[0]) * 5
	return image.Rect(0, 0, xmax, ymax)
}

func (s Scribble) At(x, y int) color.Color {
	return mazeColorMap(s.Points[y/5][x/5])
}

func (s Scribble) String() string {
	return fmt.Sprintf("Found %d matches: %s", len(s.Matches), fmt.Sprint(s.Matches))
}

// parse a scribble string from chat wars
func parseScribble(scribble string) Scribble {
	rows := strings.Split(scribble, "\n")
	var playerLocation point

	scrib := make([][]uint8, len(rows))
	for i := range rows {
		// Remove Unicode Variation Selectors
		realString := strings.Map(func(r rune) rune {
			if unicode.In(r, unicode.Variation_Selector) {
				return -1
			}
			return r
		}, rows[i])
		runes := []rune(realString)
		scrib[i] = make([]uint8, len(runes))
		fmt.Println(len(runes))
		fmt.Println(realString)
		for j, rune := range runes {
			fmt.Printf("%d: \"%c\" %+q\n", j, rune, rune)
			switch rune {
			case '\u2b1b':
				scrib[i][j] = tWALL
			case '\u2b1c':
				scrib[i][j] = tPATH
			case '\U0001f7e8':
				scrib[i][j] = 254 // player overwrites other types
				playerLocation = point{j, i}
			case '\U0001f7e9':
				scrib[i][j] = tFOUNTAIN
			case '\U0001f7e7':
				scrib[i][j] = tBONFIRE
			case '\U0001f7e6':
				scrib[i][j] = tFAMOUS
			case '\U0001f7ea':
				scrib[i][j] = tMONSTER
			default:
				scrib[i][j] = 255
			}
		}
	}
	return Scribble{scrib, playerLocation, make([]point, 0)}
}

// An Item is something we manage in a priority queue.
type Item struct {
	p        point // The value of the item; arbitrary.
	priority int   // The priority of the item in the queue.
}

func (i Item) Less(other interface{}) bool {
	// We want Pop to give us the highest, not lowest, priority so we use greater than here.
	return i.priority < other.(*Item).priority
}
