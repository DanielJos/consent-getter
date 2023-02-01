package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Event struct {
	Site        string `json:"site"`
	Key         string `json:"key"`
	NumToGet    int    `json:"numtoget"`
	QueueEndpoint string `json:"queueendpoint"`
}

type DataResponseOut struct {
	Key       string `json:"key"`
	Source    string `json:"source"`
	Datetime  string `json:"datetime"`
	Headline  string `json:"headline"`
	PageTitle string `json:"pagetitle"`
	Link      string `json:"link"`
}

func (dataOut DataResponseOut) String() (string, error) {
	bytes, err := json.Marshal(dataOut)
	if err != nil {
		log.Println("Unable to marshal data response:", err)
		return "", fmt.Errorf("Unable to marshal data response:", err)
	}
	return string(bytes), nil
}

// Add headers to the request
func addGHeaders(req *http.Request) {
	req.Host = "www.google.com"

	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("TE", "trailers")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept-Language", "en-GB,en;q=0.5")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Content-Type", "text/html; charset=utf-8")
}

// Add headers to the request
func addPageHeaders(req *http.Request) {
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("TE", "trailers")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept-Language", "en-GB,en;q=0.5")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Content-Type", "text/html; charset=utf-8")
}

// Getter struct
type Getter struct {
	rmqConnection 	*amqp.Connection
	rmqChannel    	*amqp.Channel
	httpClient   	*http.Client
}
func (getter *Getter) Close() {
	getter.rmqConnection.Close()
}
func (getter *Getter) Init(event Event) error {
	log.Println("Initialising getter...")

	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("Unable to create cookie jar:", err)
	}

	getter.httpClient = &http.Client{
		Jar: jar,
	}

	// Init RabbitMQ connection
	log.Println("Connecting to RabbitMQ:", event.QueueEndpoint)
	getter.rmqConnection, err = amqp.Dial(event.QueueEndpoint)
	if err != nil {
		log.Println("AMQP dial error:", err)
		os.Exit(1)
		return fmt.Errorf("AMQP dial error: %v", err)
	}
	getter.rmqChannel, err = getter.rmqConnection.Channel()
	if err != nil {
		log.Println("AMQP channel error: %v", err)
		os.Exit(1)
		return fmt.Errorf("AMQP channel error: %v", err)
	}

	log.Println("Getter initialised.")
	return nil
}

func (getter *Getter) HandleArticle(outRes DataResponseOut, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Println("Received article link:", outRes.Link)

	// Get the article
	req, err := http.NewRequest("GET", outRes.Link, nil)
	if err != nil {
		log.Println("Get request error:", err)
		return
	}

	cookie := &http.Cookie{
		Name:  "CONSENT",
		Value: "YES+cb.20220403-18-p0.en+FX+489",
	}

	req.AddCookie(cookie)
	addPageHeaders(req)

	res, err := getter.httpClient.Do(req)
	if err != nil {
		log.Println("Do get request error:", err)
		return
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Println("parse article html error:", err)
		return
	}

	// Get the article title
	doc.Find("head > title").First().Each(func(i int, s *goquery.Selection) {
		title := s.Text()
		log.Println("Title:", title)
		outRes.PageTitle = title
	})

	res.Body.Close()

	// Post the data
	log.Println("Posting data:", outRes)

	outJSON, err := outRes.String()
	if err != nil {
		log.Println("JSON to string error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Println("Posting data:", outRes.Headline)
	err = getter.rmqChannel.PublishWithContext(ctx,
		"consent.direct",
		"datain",
		false,
		false,
		amqp.Publishing{
			ContentType:"text/json",
			Body:        []byte(outJSON),
		},
	)
	if err != nil {
		log.Printf("AMQP publish error: %v", err)
		return
	}
}

func waitGroupTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false
	case <-time.After(timeout):
		return true
	}
}

func HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	log.Printf("Received request: %+v", request)
	log.Println("Received request body:", request.Body)

	fmt.Printf("%+v\n", request)

	
	event := Event{}
	err := json.Unmarshal([]byte(request.Body), &event)
	if err != nil {
		log.Println("Unable to unmarshal event:", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       "Unable to unmarshal event",
		}, fmt.Errorf("Unable to unmarshal event:", err)
	}
	
	// If any of the fields in the body are empty then return an error
	if event.Site == "" || event.Key == "" || event.NumToGet == 0 || event.QueueEndpoint == "" {
		log.Println("Missing required fields in request body. Request body:", request.Body)
		log.Printf("event: %+v", event)
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       "Missing required fields in request body" + request.Body,
		}, fmt.Errorf("Missing required fields in request body: " + request.Body + " :finished")
	}

	// Init the getter
	getter := Getter{}
	getter.Init(event)
	defer getter.Close()

	log.Println("Ready to start scraping")
	
	cookie := &http.Cookie{
		Name:  "CONSENT",
		Value: "YES+cb.20220403-18-p0.en+FX+489",
	}

	wg := sync.WaitGroup{}
	// Handle the article links
	
	currentTime := time.Now().Format("2006-01-02 15:04:05")

	// Scrape results for article links
	for i := 0; i < event.NumToGet; i += 10 {
		url := fmt.Sprintf(
			"https://www.google.com/search?q=site:%s&tbm=nws&source=lnt&tbs=qdr:d&sa=X&ved=2ahUKEwipsdTt2u38AhWWbcAKHYTMCwUQpwV6BAgCEBc&biw=1298&bih=778&dpr=1.82&start=%d",
			event.Site,
			i,
		)
		log.Printf("URL: %v", url)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return events.APIGatewayProxyResponse{
				StatusCode: 500,
				Body:       "Unable to create request",
			}, err
		}
		req.AddCookie(cookie)
		addGHeaders(req)

		res, err := getter.httpClient.Do(req)
		if err != nil {
			return events.APIGatewayProxyResponse{
				StatusCode: 500,
				Body:       "Unable to get response",
			}, err
		}
		defer res.Body.Close()

		log.Println("Status code:", res.StatusCode)
		if res.StatusCode == 429 {
			log.Println("Too many requests")
			return events.APIGatewayProxyResponse{
				StatusCode: 429,
				Body:       "Too many requests",
			}, nil
		}

		doc, err := goquery.NewDocumentFromReader(res.Body)
		if err != nil {
			return events.APIGatewayProxyResponse{
				StatusCode: 500,
				Body:       "Unable to parse response",
			}, err
		}

		log.Printf("Scraping results for %v", event.Site)
		doc.Find(".Gx5Zad.fP1Qef.xpd.EtOod.pkphOe").Each(func(i int, s *goquery.Selection) {
			// Find links
			link, _ := s.Find("a").Attr("href")
			link = strings.Replace(link, "/url?q=", "", 1)

			headline := s.Find("div.BNeawe.vvjwJb.AP7Wnd").First().Text()

			log.Printf("Link: %v", link)
			wg.Add(1)
			getter.HandleArticle(DataResponseOut{
				Datetime: currentTime,
				Link:     link,
				Headline: headline,
				Key: 	event.Key,
				Source: event.Site,
			}, &wg)
		})

		// Run and wait for 10 concurrent articles
		if waitGroupTimeout(&wg, 20*time.Second) {
			log.Println("Timeout waiting for article links")
			continue
		} else {
			log.Println("Articles ready")
		}
	}

	log.Println("Done.")

	// doneChan <- true
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       "Success!",
	}, nil
}

func main() {
	log.Println("Starting up")
	lambda.Start(HandleRequest)

	// body := os.Getenv("TEST_GETTER_TRIGGER_BODY")
	// log.Println("Body:", body)
	// ev := events.APIGatewayProxyRequest{
	// 	Body: body,
	// }

	// s, err := HandleRequest(context.Background(), ev)
	// if err != nil {
	// 	log.Println("Error:", err)
	// }
	// log.Println("Result:", s)
}
