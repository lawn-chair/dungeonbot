package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	cwmaze "dungeonbot/maze"

	"github.com/lawn-chair/gobot/tgbot"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/redis/go-redis/v9"
	"golang.org/x/image/font/gofont/goregular"
)

// This handler is called everytime telegram sends us a webhook event
func Handler(res http.ResponseWriter, req *http.Request) {
	// First, decode the JSON response body
	body := &tgbot.Update{}

	if err := json.NewDecoder(req.Body).Decode(body); err != nil {
		fmt.Println("could not decode request body", err)
		return
	}

	if body.Message.Photo != nil {
		fullSizeImage := tgbot.GetFullSizeImage(body.Message.Photo)
		res, err := bot.SendCommand("getFile", struct {
			FileID string `json:"file_id"`
		}{fullSizeImage})
		if err != nil {
			bot.Respond(body.Message, "Failed to get map image from Telegram server")
			fmt.Println(err)
			return
		}

		fileInfo := &tgbot.Response[tgbot.File]{}

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
		bot.Respond(body.Message, tgbot.EscapeString(m.String()))

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

		err = redisClient.Del(ctx, fmt.Sprintf("%d-Location", body.Message.Chat.ID)).Err()
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

				drawPlayerBox(composite, &cwmaze.Point{X: match.X + matches.PlayerLocation.X, Y: match.Y + matches.PlayerLocation.Y})
			}

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

			err = redisClient.Del(ctx, fmt.Sprintf("%d-Location", body.Message.Chat.ID)).Err()
			if err != nil {
				fmt.Println(err)
			}

		} else {
			bot.Respond(body.Message, "There seems to be a problem with your scribble")
		}

	} else if strings.HasPrefix(body.Message.Text, "/path") {
		maze, scribble, location, err := getPlayerState(body.Message)

		if err != nil {
			bot.Respond(body.Message, fmt.Sprint(err))
			return
		}

		if len(scribble.Matches) > 1 {
			bot.Respond(body.Message, fmt.Sprintf("Scribble matches %d locations in map.  Must be 1 to find path", len(scribble.Matches)))
			return
		}

		re, _ := regexp.Compile(`\/path([ _]([^ _\n]+)([ _](\d+))?)?`)
		thingToFind := maze.Boss
		matches := re.FindAllStringSubmatch(body.Message.Text, -1)
		if location == nil {
			location = &cwmaze.Point{X: scribble.Matches[0].X + scribble.PlayerLocation.X,
				Y: scribble.Matches[0].Y + scribble.PlayerLocation.Y}

		}

		if matches == nil {
			bot.Respond(body.Message, "Could not parse command")
			return
		}

		switch matches[0][2] {
		case "chest":
			if matches[0][4] != "" {
				num, _ := strconv.Atoi(matches[0][4])
				if num > len(maze.Chests) {
					bot.Respond(body.Message, fmt.Sprintf("Invalid chest number: %d", num))
					return
				}
				thingToFind = cwmaze.Nearest(maze.Chests, location, num)[num-1]
			} else {
				thingToFind = cwmaze.Nearest(maze.Chests, location, 1)[0]
			}
		case "mob":
			if matches[0][4] != "" {
				num, _ := strconv.Atoi(matches[0][4])
				if num > len(maze.Mobs) {
					bot.Respond(body.Message, fmt.Sprintf("Invalid mob number: %d", num))
					return
				}
				thingToFind = cwmaze.Nearest(maze.Mobs, location, num)[num-1]
			} else {
				thingToFind = cwmaze.Nearest(maze.Mobs, location, 1)[0]
			}
		}

		path, err := maze.FindPath(location, &thingToFind)
		if err != nil {
			bot.Respond(body.Message, fmt.Sprint(err))
		}

		// create the composite image with the map and scribbles highlighted
		composite := image.NewRGBA(image.Rect(0, 0, maze.Bounds().Dx(), maze.Bounds().Dy()))
		draw.Draw(composite, composite.Bounds(), maze, maze.Bounds().Bounds().Min, draw.Src)
		gc := gg.NewContextForRGBA(composite)

		drawPlayerBox(composite, location)

		for pixel := range path {
			gc.DrawLine((float64)(path[pixel].X*5+2), (float64)(path[pixel].Y*5+3),
				(float64)(path[pixel].X*5+4), (float64)(path[pixel].Y*5+3))
			gc.SetColor(color.RGBA{255, 20, 255, 255})
			gc.SetLineWidth(2)
			gc.Stroke()
		}

		bot.RespondPhoto(body.Message, composite)
	} else if strings.HasPrefix(body.Message.Text, "/mobs") {
		maze, scribble, player, err := getPlayerState(body.Message)

		if err != nil {
			bot.Respond(body.Message, fmt.Sprint(err))
			return
		}

		if len(scribble.Matches) > 1 {
			bot.Respond(body.Message, fmt.Sprintf("Scribble matches %d locations in map.  Must be 1 to find mobs", len(scribble.Matches)))
			return
		}

		if player == nil {
			player = &cwmaze.Point{X: scribble.Matches[0].X + scribble.PlayerLocation.X, Y: scribble.Matches[0].Y + scribble.PlayerLocation.Y}
		}
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

		drawPlayerBox(composite, player)

		for c := range list {
			gc.DrawCircle((float64)(list[c].X*5+2), (float64)(list[c].Y*5+2), 12)
			gc.SetColor(color.NRGBA{120, 100, 255, 180})
			gc.Fill()

			gc.SetColor(color.NRGBA{255, 255, 255, 255})
			gc.DrawString(fmt.Sprintf("%d", c+1), (float64)(list[c].X*5-2), (float64)(list[c].Y*5+8))
		}
		bot.RespondPhoto(body.Message, composite)

		for c := range list {
			bot.Respond(body.Message, fmt.Sprintf("\\/path\\_mob\\_%d path to mob at \\{%d, %d\\} \\/at\\_%d\\_%d", c+1, list[c].X, list[c].Y, list[c].X, list[c].Y))
		}
	} else if strings.HasPrefix(body.Message.Text, "/chests") {
		maze, scribble, player, err := getPlayerState(body.Message)

		if err != nil {
			bot.Respond(body.Message, fmt.Sprint(err))
			return
		}

		if len(scribble.Matches) > 1 {
			bot.Respond(body.Message, fmt.Sprintf("Scribble matches %d locations in map.  Must be 1 to find chests", len(scribble.Matches)))
			return
		}

		if player == nil {
			player = &cwmaze.Point{X: scribble.Matches[0].X + scribble.PlayerLocation.X, Y: scribble.Matches[0].Y + scribble.PlayerLocation.Y}
		}
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

		drawPlayerBox(composite, player)

		for c := range list {
			gc.DrawCircle((float64)(list[c].X*5+2), (float64)(list[c].Y*5+2), 12)
			gc.SetColor(color.NRGBA{120, 255, 100, 180})
			gc.Fill()

			gc.SetColor(color.NRGBA{0, 0, 0, 255})
			gc.DrawString(fmt.Sprintf("%d", c+1), (float64)(list[c].X*5-2), (float64)(list[c].Y*5+8))
		}
		bot.RespondPhoto(body.Message, composite)

		for c := range list {
			bot.Respond(body.Message, fmt.Sprintf("\\/path\\_chest\\_%d path to chest at \\{%d, %d\\} /at\\_%d\\_%d", c+1, list[c].X, list[c].Y, list[c].X, list[c].Y))
		}
	} else if strings.HasPrefix(body.Message.Text, "/at") {
		re, _ := regexp.Compile(`\/at[ _](\d+)[ ,_]+(\d+)`)
		matches := re.FindAllStringSubmatch(body.Message.Text, -1)
		maze, _, _, err := getPlayerState(body.Message)

		if err != nil {
			bot.Respond(body.Message, fmt.Sprint(err))
		}

		if matches == nil || matches[0][1] == "" || matches[0][2] == "" {
			bot.Respond(body.Message, "Could not parse command")
			return
		}

		x, _ := strconv.Atoi(matches[0][1])
		y, _ := strconv.Atoi(matches[0][2])

		if x >= len(maze.Pixels[0]) || y >= len(maze.Pixels) {
			bot.Respond(body.Message, "Position is outside of the maze.")
			return
		}

		location := cwmaze.Point{X: x, Y: y}
		locationJson, err := json.Marshal(location)
		if err != nil {
			fmt.Println(err)
		}

		err = redisClient.Set(ctx, fmt.Sprintf("%d-Location", body.Message.Chat.ID), locationJson, 0).Err()
		if err != nil {
			fmt.Println(err)
			bot.Respond(body.Message, "Failed to save location")
		} else {
			bot.Respond(body.Message, tgbot.EscapeString(fmt.Sprintf("Location set: %s", location)))
		}
	} else {
		bot.Respond(body.Message, "Try forwarding a map or scribble of a dungeon")
	}

	// log a confirmation message if the message is sent successfully
	fmt.Println("reply sent")
}

func getPlayerState(message tgbot.Message) (*cwmaze.Maze, *cwmaze.Scribble, *cwmaze.Point, error) {
	maze := &cwmaze.Maze{}
	if err := getFromRedis(maze, fmt.Sprint(message.Chat.ID)); err != nil {
		return nil, nil, nil, errors.New("no map found, please forward map before taking other actions")
	}

	scribble := &cwmaze.Scribble{}
	if err := getFromRedis(scribble, fmt.Sprintf("%d-Scribble", message.Chat.ID)); err != nil {
		return maze, nil, nil, errors.New("no scribble found, please forward scribble before taking other actions")
	}

	location := &cwmaze.Point{}
	if err := getFromRedis(location, fmt.Sprintf("%d-Location", message.Chat.ID)); err != nil {
		location = nil
	}

	return maze, scribble, location, nil
}

func drawPlayerBox(composite *image.RGBA, player *cwmaze.Point) {
	gc := gg.NewContextForRGBA(composite)

	gc.DrawRectangle(float64(player.X)*5, float64(player.Y)*5, 5, 5)
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
var bot tgbot.Bot
var ctx = context.Background()

func main() {
	port := getEnv("PORT", "3000")

	redisUrl := getEnv("REDIS_URL", "redis://localhost:6379")
	opt, _ := redis.ParseURL(redisUrl)
	redisClient = redis.NewClient(opt)

	pong, err := redisClient.Ping(context.Background()).Result()
	fmt.Println(pong, err)

	bot = tgbot.Bot{API_KEY: getEnv("TG_API_KEY", "abcd:1234")}
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
