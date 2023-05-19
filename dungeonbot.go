package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	cwmaze "dungeonbot/maze"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/redis/go-redis/v9"
	"golang.org/x/image/font/gofont/goregular"
)

type TGBot struct {
	API_KEY string
}

func (t TGBot) DownloadFile(filePath string) (*http.Response, error) {
	url := "https://api.telegram.org/file/bot" + t.API_KEY + "/" + filePath
	fmt.Println("Downloading", url)
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("unexpected status" + res.Status)
	}
	return res, nil
}

func (t TGBot) SendCommand(cmd string, body interface{}) (*http.Response, error) {
	// Create the JSON body from the struct
	reqBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	// Send a post request with your token
	url := "https://api.telegram.org/bot" + t.API_KEY + "/" + cmd
	res, err := http.Post(url, "application/json", bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("unexpected status " + res.Status)
	}
	return res, nil
}

func (t TGBot) Respond(m TGMessage, s string) (*http.Response, error) {
	return t.SendCommand("sendMessage", struct {
		ChatID    int64  `json:"chat_id"`
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}{m.Chat.ID, s, "MarkdownV2"})
}

func (t TGBot) SetWebhook(url string) (*http.Response, error) {
	return t.SendCommand("setWebhook", struct {
		Url string `json:"url"`
	}{url})
}

func EscapeString(s string) string {
	re := regexp.MustCompile(`(?m)([_\*\[\]()~\x60>#+\-=|{}!\.])`)
	return re.ReplaceAllString(s, "\\$1")
}

type SendFile struct {
	field    string
	filename string
	reader   io.Reader
}

func (t TGBot) SendFiles(cmd string, body interface{}, files []SendFile) (*http.Response, error) {
	r, w := io.Pipe()
	m := multipart.NewWriter(w)

	url := "https://api.telegram.org/bot" + t.API_KEY + "/" + cmd

	go func() {
		defer w.Close()
		defer m.Close()

		rv := reflect.ValueOf(body)
		t := rv.Type()
		for i := range reflect.VisibleFields(t) {
			name, _ := t.Field(i).Tag.Lookup("json") // Check for errors?
			value := ""
			switch rv.Field(i).Kind() {
			case reflect.Int:
				value = strconv.FormatInt(rv.Field(i).Int(), 10)
			case reflect.Int64:
				value = strconv.FormatInt(rv.Field(i).Int(), 10)
			case reflect.String:
				value = rv.Field(i).String()
			case reflect.Bool:
				value = strconv.FormatBool(rv.Field(i).Bool())
			}
			fmt.Println("encoding field: ", name, value)
			_ = m.WriteField(name, value)
		}

		for i := range files {
			part, err := m.CreateFormFile(files[i].field, files[i].filename)
			if err != nil {
				log.Fatal(err)
				return
			}
			io.Copy(part, files[i].reader)
		}

	}()

	req, _ := http.NewRequest("POST", url, r) // as you can see I have passed the pipe reader here
	req.Header.Set("Content-Type", m.FormDataContentType())
	res, err := http.DefaultClient.Do(req) // do the request. The program will stop here until the upload is done
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	/*
		data, _ := io.ReadAll(resp.Body) // read the results
		fmt.Println(string(data))
	*/
	return res, nil
}

func (t TGBot) RespondPhoto(m TGMessage, i image.Image) (*http.Response, error) {
	buf := new(bytes.Buffer)
	png.Encode(buf, i)

	sendPic := []SendFile{
		{
			"photo",
			"photo.png",
			buf,
		},
	}

	return bot.SendFiles("sendPhoto", struct {
		ChatID int64 `json:"chat_id"`
	}{m.Chat.ID}, sendPic)
}

type TGFile struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
}

type TGPhotoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type TGMessage struct {
	Text string `json:"text"`
	ID   int64  `json:"message_id"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Photo []TGPhotoSize `json:"photo"`
}

type TGResponse[T any] struct {
	Ok     bool `json:"ok"`
	Result T    `json:"result"`
}

type TGUpdate struct {
	Message TGMessage `json:"message"`
}

func getFullSizeImage(photos []TGPhotoSize) string {
	largest := 0
	var fullSizeImage *TGPhotoSize

	for i := range photos {
		if photos[i].Width > largest {
			fullSizeImage = &photos[i]
			largest = photos[i].Width
		}
	}

	return fullSizeImage.FileID
}

// This handler is called everytime telegram sends us a webhook event
func Handler(res http.ResponseWriter, req *http.Request) {
	// First, decode the JSON response body
	body := &TGUpdate{}

	if err := json.NewDecoder(req.Body).Decode(body); err != nil {
		fmt.Println("could not decode request body", err)
		return
	}

	if body.Message.Photo != nil {
		fullSizeImage := getFullSizeImage(body.Message.Photo)
		res, err := bot.SendCommand("getFile", struct {
			FileID string `json:"file_id"`
		}{fullSizeImage})
		if err != nil {
			bot.Respond(body.Message, "Failed to get map image from Telegram server")
			fmt.Println(err)
			return
		}

		fileInfo := &TGResponse[TGFile]{}

		if err := json.NewDecoder(res.Body).Decode(fileInfo); err != nil {
			bot.Respond(body.Message, "Failed to get map image from Telegram server")
			fmt.Println("could not decode getFile response", err)
			return
		}

		fileRes, err := bot.DownloadFile(fileInfo.Result.FilePath)
		if err != nil {
			bot.Respond(body.Message, "Failed to get map image from Telegram server")
			fmt.Println(err)
			return
		}

		m := cwmaze.Maze{}
		mazeImage, _, err := image.Decode(fileRes.Body)
		if err != nil {
			bot.Respond(body.Message, "Failed to decode map image")
			fmt.Println(err)
			return
		}

		m.Load(mazeImage)

		bot.RespondPhoto(body.Message, m)

		fmt.Println("m.String(): ", m.String(), m)
		bot.Respond(body.Message, EscapeString(m.String()))

		mazeJson, err := json.Marshal(m)
		if err != nil {
			fmt.Println(err)
		}

		err = redisClient.Set(ctx, fmt.Sprint(body.Message.Chat.ID), mazeJson, 0).Err()
		if err != nil {
			fmt.Println(err)
			bot.Respond(body.Message, "Failed to save map")
		}

		err = redisClient.Del(ctx, fmt.Sprintf("%d-Scribble", body.Message.Chat.ID)).Err()
		if err != nil {
			fmt.Println(err)
		}

	} else if strings.Contains(body.Message.Text, "You stopped and tried to mark your way on paper.") {

		sections := strings.Split(body.Message.Text, "\n\n")

		maze := &cwmaze.Maze{}
		mazeJson, err := redisClient.Get(ctx, fmt.Sprint(body.Message.Chat.ID)).Result()
		if err != nil {
			if err == redis.Nil {
				bot.Respond(body.Message, "No map found, please forward map before sending a scribble")
			} else {
				bot.Respond(body.Message, "Error fetching map")
				fmt.Println("error fetching map from redis", err)
			}
			return
		}

		if err := json.NewDecoder(strings.NewReader(mazeJson)).Decode(maze); err != nil {
			fmt.Println("could not decode request body", err)
			return
		}

		if len(sections) > 1 {
			matches := maze.SearchByScribble(sections[1])

			fmt.Println(matches)

			bot.Respond(body.Message, matches.String())

			/* This will send a picture of the scribble
			buf := new(bytes.Buffer)
			png.Encode(buf, matches)

			sendPic := []SendFile{
				{
					"photo",
					"scribble.png",
					buf,
				},
			}
			bot.SendFiles("sendPhoto", struct {
				ChatID int64 `json:"chat_id"`
			}{body.Message.Chat.ID}, sendPic)
			*/

			// create the composite image with the map and scribbles highlighted
			composite := image.NewRGBA(image.Rect(0, 0, maze.Bounds().Dx(), maze.Bounds().Dy()))
			draw.Draw(composite, composite.Bounds(), maze, maze.Bounds().Bounds().Min, draw.Src)

			gc := gg.NewContextForRGBA(composite)
			for _, match := range matches.Matches {
				playerX := (float64)(match.X+matches.PlayerLocation.X) * 5
				playerY := (float64)(match.Y+matches.PlayerLocation.Y) * 5
				gc.DrawCircle(playerX, playerY, 20)
				gc.SetRGBA(100, 255, 100, 150)
				gc.SetColor(color.NRGBA{100, 255, 100, 150})
				gc.Fill()
			}

			drawPlayerBox(composite, matches)

			bot.RespondPhoto(body.Message, composite)

			if len(matches.Matches) == 1 {
				_, err := bot.Respond(body.Message, "*Location Found\\!* Try these commands for more help:\n\n\\/path to find a path to the boss using fountains\n\\/path\\_chest for a path to the nearest chest\n\\/path\\_mob for a path to the nearest mob\n\nFor more options try \\/mobs or \\/chests")
				if err != nil {
					fmt.Println(err)
				}
				bot.Respond(body.Message, fmt.Sprintf("Player at: \\{%d, %d\\}", matches.Matches[0].X+matches.PlayerLocation.X, matches.Matches[0].Y+matches.PlayerLocation.Y))
			} else {
				bot.Respond(body.Message, fmt.Sprintf("Found %d locations matching scribble", len(matches.Matches)))
			}

			matchJson, err := json.Marshal(matches)
			if err != nil {
				fmt.Println(err)
			}

			err = redisClient.Set(ctx, fmt.Sprintf("%d-Scribble", body.Message.Chat.ID), matchJson, 0).Err()
			if err != nil {
				fmt.Println(err)
				bot.Respond(body.Message, "Failed to save scribble")
			}

		} else {
			bot.Respond(body.Message, "There seems to be a problem with your scribble")
		}

	} else if strings.HasPrefix(body.Message.Text, "/path") {
		maze := &cwmaze.Maze{}
		if err := getFromRedis(maze, fmt.Sprint(body.Message.Chat.ID)); err != nil {
			bot.Respond(body.Message, "No map found, please forward map before finding path")
			return
		}

		scribble := &cwmaze.Scribble{}
		if err := getFromRedis(scribble, fmt.Sprintf("%d-Scribble", body.Message.Chat.ID)); err != nil {
			bot.Respond(body.Message, "No scribble found, please forward scribble before finding a path")
			return
		}

		if len(scribble.Matches) > 1 {
			bot.Respond(body.Message, fmt.Sprintf("Scribble matches %d locations in map.  Must be 1 to find path", len(scribble.Matches)))
			return
		}

		re, _ := regexp.Compile(`\/path([ _]([^ _\n]+)([ _](\d+))?)?`)
		thingToFind := maze.Boss
		matches := re.FindAllStringSubmatch(body.Message.Text, -1)
		location := cwmaze.Point{X: scribble.Matches[0].X + scribble.PlayerLocation.X,
			Y: scribble.Matches[0].Y + scribble.PlayerLocation.Y}

		if matches == nil {
			bot.Respond(body.Message, "Could not parse command")
			return
		}

		switch matches[0][2] {
		case "chest":
			if matches[0][4] != "" {
				num, _ := strconv.Atoi(matches[0][4])
				thingToFind = cwmaze.Nearest(maze.Chests, location, num)[num-1]
			} else {
				thingToFind = cwmaze.Nearest(maze.Chests, location, 1)[0]
			}
		case "mob":
			if matches[0][4] != "" {
				num, _ := strconv.Atoi(matches[0][4])
				thingToFind = cwmaze.Nearest(maze.Mobs, location, num)[num-1]
			} else {
				thingToFind = cwmaze.Nearest(maze.Mobs, location, 1)[0]
			}
		}

		path, err := maze.FindPath(location, thingToFind)
		if err != nil {
			bot.Respond(body.Message, fmt.Sprint(err))
		}

		// create the composite image with the map and scribbles highlighted
		composite := image.NewRGBA(image.Rect(0, 0, maze.Bounds().Dx(), maze.Bounds().Dy()))
		draw.Draw(composite, composite.Bounds(), maze, maze.Bounds().Bounds().Min, draw.Src)
		gc := gg.NewContextForRGBA(composite)

		drawPlayerBox(composite, *scribble)

		for pixel := range path {
			gc.DrawLine((float64)(path[pixel].X*5+2), (float64)(path[pixel].Y*5+3),
				(float64)(path[pixel].X*5+4), (float64)(path[pixel].Y*5+3))
			gc.SetColor(color.RGBA{255, 20, 255, 255})
			gc.SetLineWidth(2)
			gc.Stroke()
		}

		bot.RespondPhoto(body.Message, composite)

	} else if strings.HasPrefix(body.Message.Text, "/mobs") {
		maze := &cwmaze.Maze{}
		if err := getFromRedis(maze, fmt.Sprint(body.Message.Chat.ID)); err != nil {
			bot.Respond(body.Message, "No map found, please forward map before finding path")
			return
		}

		scribble := &cwmaze.Scribble{}
		if err := getFromRedis(scribble, fmt.Sprintf("%d-Scribble", body.Message.Chat.ID)); err != nil {
			bot.Respond(body.Message, "No scribble found, please forward scribble before finding a path to boss")
			return
		}

		if len(scribble.Matches) > 1 {
			bot.Respond(body.Message, fmt.Sprintf("Scribble matches %d locations in map.  Must be 1 to find mobs", len(scribble.Matches)))
			return
		}

		player := cwmaze.Point{X: scribble.Matches[0].X + scribble.PlayerLocation.X, Y: scribble.Matches[0].Y + scribble.PlayerLocation.Y}
		list := cwmaze.Nearest(maze.Mobs, player, 5)

		font, err := truetype.Parse(goregular.TTF)
		if err != nil {
			log.Fatal(err)
		}

		face := truetype.NewFace(font, &truetype.Options{Size: 16})

		composite := image.NewRGBA(image.Rect(0, 0, maze.Bounds().Dx(), maze.Bounds().Dy()))
		draw.Draw(composite, composite.Bounds(), maze, maze.Bounds().Bounds().Min, draw.Src)
		gc := gg.NewContextForRGBA(composite)
		gc.SetFontFace(face)

		drawPlayerBox(composite, *scribble)

		for c := range list {
			gc.DrawCircle((float64)(list[c].X*5+2), (float64)(list[c].Y*5+2), 12)
			gc.SetColor(color.NRGBA{120, 100, 255, 180})
			gc.Fill()

			gc.SetColor(color.NRGBA{255, 255, 255, 255})
			gc.DrawString(fmt.Sprintf("%d", c+1), (float64)(list[c].X*5-2), (float64)(list[c].Y*5+8))
		}
		bot.RespondPhoto(body.Message, composite)

		for c := range list {
			bot.Respond(body.Message, fmt.Sprintf("\\/path\\_mob\\_%d path to mob at \\{%d, %d\\}", c+1, list[c].X, list[c].Y))
		}
	} else if strings.HasPrefix(body.Message.Text, "/chests") {
		maze := &cwmaze.Maze{}
		if err := getFromRedis(maze, fmt.Sprint(body.Message.Chat.ID)); err != nil {
			bot.Respond(body.Message, "No map found, please forward map before finding path")
			return
		}

		scribble := &cwmaze.Scribble{}
		if err := getFromRedis(scribble, fmt.Sprintf("%d-Scribble", body.Message.Chat.ID)); err != nil {
			bot.Respond(body.Message, "No scribble found, please forward scribble before finding a path")
			return
		}

		if len(scribble.Matches) > 1 {
			bot.Respond(body.Message, fmt.Sprintf("Scribble matches %d locations in map.  Must be 1 to find chests", len(scribble.Matches)))
			return
		}

		player := cwmaze.Point{X: scribble.Matches[0].X + scribble.PlayerLocation.X, Y: scribble.Matches[0].Y + scribble.PlayerLocation.Y}
		list := cwmaze.Nearest(maze.Chests, player, 5)

		font, err := truetype.Parse(goregular.TTF)
		if err != nil {
			log.Fatal(err)
		}

		face := truetype.NewFace(font, &truetype.Options{Size: 16})

		composite := image.NewRGBA(image.Rect(0, 0, maze.Bounds().Dx(), maze.Bounds().Dy()))
		draw.Draw(composite, composite.Bounds(), maze, maze.Bounds().Bounds().Min, draw.Src)
		gc := gg.NewContextForRGBA(composite)
		gc.SetFontFace(face)

		drawPlayerBox(composite, *scribble)

		for c := range list {
			gc.DrawCircle((float64)(list[c].X*5+2), (float64)(list[c].Y*5+2), 12)
			gc.SetColor(color.NRGBA{120, 255, 100, 180})
			gc.Fill()

			gc.SetColor(color.NRGBA{0, 0, 0, 255})
			gc.DrawString(fmt.Sprintf("%d", c+1), (float64)(list[c].X*5-2), (float64)(list[c].Y*5+8))
		}
		bot.RespondPhoto(body.Message, composite)

		for c := range list {
			bot.Respond(body.Message, fmt.Sprintf("\\/path\\_chest\\_%d path to chest at \\{%d, %d\\}", c+1, list[c].X, list[c].Y))
		}

	} else {
		bot.Respond(body.Message, "Try forwarding a map or scribble of a dungeon")
	}

	// log a confirmation message if the message is sent successfully
	fmt.Println("reply sent")
}

func drawPlayerBox(composite *image.RGBA, scribble cwmaze.Scribble) {
	gc := gg.NewContextForRGBA(composite)

	playerX := (float64)(scribble.Matches[0].X+scribble.PlayerLocation.X) * 5
	playerY := (float64)(scribble.Matches[0].Y+scribble.PlayerLocation.Y) * 5

	gc.DrawRectangle(playerX, playerY, 5, 5)
	gc.SetColor(color.RGBA{255, 20, 255, 255})
	gc.Fill()
}

func getFromRedis[T any](obj *T, key string) error {
	jsonVal, err := redisClient.Get(ctx, key).Result()
	if err != nil {
		fmt.Printf("error fetching %s from redis: %s\n", key, err)
		return err
	}

	if err := json.NewDecoder(strings.NewReader(jsonVal)).Decode(obj); err != nil {
		fmt.Println("could not decode request body", err)
		return err
	}

	return nil
}

func getEnv(key string, fallback string) string {
	if value, found := os.LookupEnv(key); found {
		return value
	}
	return fallback
}

var redisClient *redis.Client
var bot TGBot
var ctx = context.Background()

func main() {
	port := getEnv("PORT", "3000")

	redisUrl := getEnv("REDIS_URL", "redis://localhost:6379")
	opt, _ := redis.ParseURL(redisUrl)
	redisClient = redis.NewClient(opt)

	pong, err := redisClient.Ping(context.Background()).Result()
	fmt.Println(pong, err)

	bot = TGBot{API_KEY: getEnv("TG_API_KEY", "abcd:1234")}
	if getEnv("GO_ENV", "development") == "production" {
		_, err = bot.SetWebhook("https://happydungeon.fly.dev/" + getEnv("TG_WEBHOOK", ""))
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println("Webhook set")
		}
	}

	http.ListenAndServe(":"+port, http.HandlerFunc(Handler))
}
