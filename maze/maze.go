package cwmaze

import (
	"fmt"
	"image"
	"image/color"
	"math"
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
	Pixels [][]uint8     `json:"pixels"`
	Types  map[uint8]int `json:"types"`
	Boss   point         `json:"boss"`
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
			if p == tBOSS {
				m.Boss = point{x / 5, y / 5}
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
						//fmt.Println("no match!")
						// fountain and chest look the same in a scribble
						if !(state.Points[sy][sx] == tFOUNTAIN &&
							m.Pixels[y+sy][x+sx] == tCHEST) {
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
		return 3
	default:
		return 2
	}
}

func (m Maze) FindPathToBossFrom(s *Scribble) []point {
	start := Item{
		point{s.Matches[0].X + s.PlayerLocation.X, s.Matches[0].Y + s.PlayerLocation.Y},
		0,
	}

	frontier := pqueue.New(0)

	frontier.Enqueue(&start)

	came_from := make(map[point]point)
	cost_so_far := make(map[point]int)

	came_from[start.p] = start.p
	cost_so_far[start.p] = 0

	for frontier.Len() > 0 {
		current := frontier.Dequeue().(*Item)
		if current.p == m.Boss {
			break
		}
		fmt.Println("At: ", current.p)
		for _, next := range m.neighbors(current.p) {
			fmt.Println("Checking neighbor: ", next)
			new_cost := cost_so_far[current.p] + travelCost(m.Pixels[next.Y][next.X])
			_, exists := cost_so_far[next]
			if !exists || new_cost < cost_so_far[next] {
				cost_so_far[next] = new_cost
				priority := new_cost + heuristic(next, m.Boss)
				frontier.Enqueue(&Item{p: next, priority: priority})
				came_from[next] = current.p
			}
		}
	}
	fmt.Println("Found path, making string")
	//last := m.Boss
	path := []point{m.Boss}
	for last := came_from[m.Boss]; last != start.p; last = came_from[last] {
		path = append(path, last)
	}
	path = append(path, start.p)
	return path
	//return fmt.Sprint(path)
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
				scrib[i][j] = 255 // player overwrites other types
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
