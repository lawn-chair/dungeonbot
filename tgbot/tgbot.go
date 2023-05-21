package tgbot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
)

type Bot struct {
	API_KEY string
}

func (t Bot) DownloadFile(filePath string) (*http.Response, error) {
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

func (t Bot) SendCommand(cmd string, body interface{}) (*http.Response, error) {
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

func (t Bot) Respond(m Message, s string) (*http.Response, error) {
	return t.SendCommand("sendMessage", struct {
		ChatID    int64  `json:"chat_id"`
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}{m.Chat.ID, s, "MarkdownV2"})
}

func (t Bot) SetWebhook(url string) (*http.Response, error) {
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

func (t Bot) SendFiles(cmd string, body interface{}, files []SendFile) (*http.Response, error) {
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

func (t Bot) RespondPhoto(m Message, i image.Image) (*http.Response, error) {
	buf := new(bytes.Buffer)
	png.Encode(buf, i)

	sendPic := []SendFile{
		{
			"photo",
			"photo.png",
			buf,
		},
	}

	return t.SendFiles("sendPhoto", struct {
		ChatID int64 `json:"chat_id"`
	}{m.Chat.ID}, sendPic)
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
}

type PhotoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Message struct {
	Text string `json:"text"`
	ID   int64  `json:"message_id"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Photo []PhotoSize `json:"photo"`
}

type Response[T any] struct {
	Ok     bool `json:"ok"`
	Result T    `json:"result"`
}

type Update struct {
	Message Message `json:"message"`
}

func GetFullSizeImage(photos []PhotoSize) string {
	largest := 0
	var fullSizeImage *PhotoSize

	for i := range photos {
		if photos[i].Width > largest {
			fullSizeImage = &photos[i]
			largest = photos[i].Width
		}
	}

	return fullSizeImage.FileID
}
