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
	"strconv"
	"strings"

	cwmaze "dungeonbot/maze"

	"github.com/redis/go-redis/v9"
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
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}{m.Chat.ID, s})
}

func (t TGBot) SetWebhook(url string) (*http.Response, error) {
	return t.SendCommand("setWebhook", struct {
		Url string `json:"url"`
	}{url})
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

// Create a struct that mimics the webhook response body
// https://core.telegram.org/bots/api#update
type webhookReqBody struct {
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
	body := &webhookReqBody{}

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
		bot.Respond(body.Message, m.String())

		buf := new(bytes.Buffer)
		png.Encode(buf, m)

		sendMaze := []SendFile{
			{
				"photo",
				"maze.png",
				buf,
			},
		}
		bot.SendFiles("sendPhoto", struct {
			ChatID int64 `json:"chat_id"`
		}{body.Message.Chat.ID}, sendMaze)

		mazeJson, err := json.Marshal(m)
		if err != nil {
			fmt.Println(err)
		}

		err = redisClient.Set(ctx, fmt.Sprint(body.Message.Chat.ID), mazeJson, 0).Err()
		if err != nil {
			fmt.Println(err)
			bot.Respond(body.Message, "Failed to save map")
		}

	} else if strings.Contains(body.Message.Text, "You stopped and tried to mark your way on paper.") {

		sections := strings.Split(body.Message.Text, "\n\n")

		maze := &cwmaze.Maze{}
		mazeJson, err := redisClient.Get(ctx, fmt.Sprint(body.Message.Chat.ID)).Result()
		if err != nil {
			if err == redis.Nil {
				bot.Respond(body.Message, "No map found, please forward map before sending a scribble.")
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
			for _, match := range matches.Matches {
				drawRect(composite, color.RGBA{255, 25, 25, 255},
					match.X*5, match.Y*5,
					match.X*5+matches.Bounds().Dx(), match.Y*5+matches.Bounds().Dy())
			}

			buf := new(bytes.Buffer)
			png.Encode(buf, composite)

			sendPic := []SendFile{
				{
					"photo",
					"matches.png",
					buf,
				},
			}
			bot.SendFiles("sendPhoto", struct {
				ChatID int64 `json:"chat_id"`
			}{body.Message.Chat.ID}, sendPic)

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
			bot.Respond(body.Message, "No scribble found, please forward scribble before finding a path to boss.")
			return
		}

		if len(scribble.Matches) > 1 {
			bot.Respond(body.Message, fmt.Sprintf("Scribble matches %d locations in map.  Must be 1 to find path.", len(scribble.Matches)))
			return
		}

		//bot.Respond(body.Message, "Not implemented")
		path := maze.FindPathToBossFrom(scribble)
		// create the composite image with the map and scribbles highlighted
		composite := image.NewRGBA(image.Rect(0, 0, maze.Bounds().Dx(), maze.Bounds().Dy()))
		draw.Draw(composite, composite.Bounds(), maze, maze.Bounds().Bounds().Min, draw.Src)

		for pixel := range path {
			drawHLine(composite, color.RGBA{255, 20, 255, 255},
				path[pixel].X*5+2, path[pixel].Y*5+3, path[pixel].X*5+4)
		}

		buf := new(bytes.Buffer)
		png.Encode(buf, composite)

		sendPic := []SendFile{
			{
				"photo",
				"path.png",
				buf,
			},
		}
		bot.SendFiles("sendPhoto", struct {
			ChatID int64 `json:"chat_id"`
		}{body.Message.Chat.ID}, sendPic)

		//bot.Respond(body.Message, fmt.Sprint(path))

	} else {
		bot.Respond(body.Message, "Try forwarding a map or scribble of a dungeon")
	}

	// log a confirmation message if the message is sent successfully
	fmt.Println("reply sent")
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

// HLine draws a horizontal line
func drawHLine(img *image.RGBA, col color.Color, x1, y, x2 int) {
	for ; x1 <= x2; x1++ {
		img.Set(x1, y, col)
		img.Set(x1, y-1, col)
	}
}

// VLine draws a veritcal line
func drawVLine(img *image.RGBA, col color.Color, x, y1, y2 int) {
	for ; y1 <= y2; y1++ {
		img.Set(x, y1, col)
		img.Set(x-1, y1, col)
	}
}

// Rect draws a rectangle utilizing HLine() and VLine()
func drawRect(img *image.RGBA, col color.Color, x1, y1, x2, y2 int) {
	drawHLine(img, col, x1, y1, x2)
	drawHLine(img, col, x1, y2, x2)
	drawVLine(img, col, x1, y1, y2)
	drawVLine(img, col, x2, y1, y2)
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
	_, err = bot.SetWebhook("https://happydungeon.fly.dev/" + getEnv("TG_WEBHOOK", ""))
	if err != nil {
		fmt.Println(err)
	}

	http.ListenAndServe(":"+port, http.HandlerFunc(Handler))
}
