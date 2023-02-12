package cwmaze

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"log"
	"os"
	"testing"
)

func TestNeighbors(t *testing.T) {
	m := setup()
	x := point{1, 1}

	n := m.neighbors(x)
	fmt.Println(n)
	if len(n) != 2 {
		t.Fatalf("len(neighbors({5, 6})) = %q, want 2", len(n))
	}
	x = point{19, 5}
	n = m.neighbors(x)
	fmt.Println(n)
	if len(n) != 3 {
		t.Fatalf("len(neighbors({5, 6})) = %q, want 3", len(n))
	}
}

func TestPathFinder(t *testing.T) {
	m := setup()
	s := Scribble{
		PlayerLocation: point{0, 0},
		Matches:        []point{{89, 81}},
	}

	fmt.Println(m.FindPathToBossFrom(&s))

	s = Scribble{
		PlayerLocation: point{0, 0},
		Matches:        []point{{87, 82}},
	}

	fmt.Println(m.FindPathToBossFrom(&s))

	s = Scribble{
		PlayerLocation: point{0, 0},
		Matches:        []point{{1, 1}},
	}

	fmt.Println(m.FindPathToBossFrom(&s))

	s = Scribble{
		PlayerLocation: point{0, 1},
		Matches:        []point{{1, 4}},
	}

	fmt.Println(m.FindPathToBossFrom(&s))

}

func setup() Maze {
	f, err := os.Open("test.jpeg")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	imData, _, _ := image.Decode(f)

	m := Maze{}
	m.Load(imData)
	fmt.Println(m)
	return m

}
