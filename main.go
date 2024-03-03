package main

import (
	"bytes"
	"cmp"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"log"

	"github.com/go-sql-driver/mysql"
	"github.com/ikawaha/kagome/tokenizer"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/net/html"
)

var postsCount, _ = strconv.Atoi(cmp.Or(os.Getenv("POSTS_COUNT"), "2100"))

var db *sqlx.DB

func main() {
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		log.Fatalf("failed to load location: %v", err)
	}

	conf := mysql.Config{
		User:                 cmp.Or(os.Getenv("NS_MARIADB_USER"), "root"),
		Passwd:               cmp.Or(os.Getenv("NS_MARIADB_PASSWORD"), "root"),
		Net:                  "tcp",
		Addr:                 cmp.Or(os.Getenv("NS_MARIADB_HOSTNAME"), "mariadb") + ":3306",
		DBName:               cmp.Or(os.Getenv("NS_MARIADB_DATABASE"), "blog_relay"),
		AllowNativePasswords: true,
		ParseTime:            true,
		Loc:                  jst,
		Collation:            "utf8mb4_unicode_ci",
	}
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second * time.Duration(i))
		db, err = sqlx.Connect("mysql", conf.FormatDSN())
		if err != nil {
			log.Printf("failed to connect db: %v", err)
			continue
		}
		break
	}

	log.Println("initializing db")
	err = initializeData()
	if err != nil {
		log.Fatalf("failed to initialize data: %v", err)
	}

	temp, err := template.ParseFS(templateFS, "view/*.html")
	if err != nil {
		log.Fatalf("failed to parse template: %v", err)
	}
	t = &Template{
		templates: temp.Funcs(template.FuncMap{"add": func(a, b int) int { return a + b }}),
	}

	e := echo.New()
	e.Renderer = t

	e.Use(middleware.Recover())
	e.Use(middleware.Logger())

	e.GET("/", generateHandler)
	e.POST("/like", postLikeHandler)
	e.GET("/like", getLikesHandler)

	e.Start(":8080")
}

//go:embed view/*.html
var templateFS embed.FS

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data any, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

var t *Template

func generateHandler(c echo.Context) error {
	var selectedSentenceType SentenceType
	err := db.Get(&selectedSentenceType, "SELECT word_types FROM sentences ORDER BY RAND() LIMIT 1")
	if err != nil {
		log.Printf("failed to get sentence types: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get sentence types")
	}

	var res string
	for _, wordTypeStr := range strings.Split(selectedSentenceType.WordTypes, ",") {
		if wordTypeStr == "" {
			continue
		}
		wordTypeStr = strings.TrimLeft(strings.TrimRight(wordTypeStr, ")"), "(")
		wordTypes := strings.Split(wordTypeStr, "|")
		var selectedWord Word
		err = db.Get(&selectedWord, "SELECT word FROM words WHERE word_type = ? AND word_type2 = ? ORDER BY RAND() LIMIT 1", wordTypes[0], wordTypes[1])
		if errors.Is(err, sql.ErrNoRows) {
			// 発生しないはずだが、なぜか起きるのでもう一回実行する
			return generateHandler(c)
		}
		if err != nil {
			log.Printf("failed to get words: %v: %v", wordTypes, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get words")
		}
		res += selectedWord.Word
	}

	// return c.HTML(http.StatusOK, fmt.Sprintf(responseHTML, res, res))
	return c.Render(http.StatusOK, "index.html", map[string]any{"THEME": res})
}

type likeRequest struct {
	Theme string `json:"theme"`
}

func postLikeHandler(c echo.Context) error {
	var req likeRequest
	c.Bind(&req)

	_, err := db.Exec("INSERT INTO likes (theme) VALUES (?)", req.Theme)
	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		if mysqlErr.Number == 1062 {
			return c.NoContent(http.StatusOK)
		}
	}
	if err != nil {
		log.Printf("failed to insert like: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert like")
	}

	return c.NoContent(http.StatusOK)
}

const perPage = 20

func getLikesHandler(c echo.Context) error {
	pageStr := c.QueryParam("page")
	page := 0
	if pageStr == "" {
		page = 0
	} else {
		var err error
		page, err = strconv.Atoi(pageStr)
		if err != nil {
			log.Printf("failed to parse page: %v", err)
			return echo.NewHTTPError(http.StatusBadRequest, "failed to parse page")
		}
	}

	var likes []string
	err := db.Select(&likes, "SELECT theme FROM likes ORDER BY id DESC LIMIT ? OFFSET ?", perPage+1, page*perPage)
	if err != nil {
		log.Printf("failed to get likes: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get likes")
	}

	var allLikesCount int
	err = db.Get(&allLikesCount, "SELECT count(*) FROM likes")
	if err != nil {
		log.Printf("failed to get likes count: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get likes count")
	}

	return c.Render(http.StatusOK, "likes.html",
		map[string]any{
			"LIKES":       likes[:min(perPage, len(likes))],
			"NEXT_PAGE":   page + 1,
			"PREV_PAGE":   page - 1,
			"NEXT_EXIST":  len(likes) > perPage,
			"PAGES_COUNT": allLikesCount/perPage + 1,
		})
}

type Word struct {
	Word      string `db:"word,omitempty"`
	WordType  string `db:"word_type,omitempty"`
	WordType2 string `db:"word_type2,omitempty"`
}

type SentenceType struct {
	WordTypes string `db:"word_types,omitempty"`
}

type feature struct {
	WordType  string `db:"word_type,omitempty"`
	WordType2 string `db:"word_type2,omitempty"`
}

func scrape(client *http.Client, i int) string {
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "https", Host: "trap.jp", Path: fmt.Sprintf("/post/%d", i)},
	}
	res, err := client.Do(req)
	if err != nil {
		log.Println(err)
	}
	defer res.Body.Close()

	tk := html.NewTokenizer(res.Body)

	var title string
	for {
		tokenType := tk.Next()
		if tokenType == html.ErrorToken {
			break
		}

		tagName, _ := tk.TagName()
		if bytes.Equal(tagName, []byte("title")) && tokenType == html.StartTagToken {
			_ = tk.Next()
			title = string(bytes.TrimRight(bytes.TrimSpace(tk.Text()), "| 東京工業大学デジタル創作同好会traP"))
			break
		}
	}

	return title
}

func tokenize(title string, japaneseTokenizer *tokenizer.Tokenizer) (map[string]*Word, []feature) {
	tokens := japaneseTokenizer.Tokenize(title)
	wordMap := make(map[string]*Word, len(tokens))
	features := make([]feature, 0, len(tokens))
	for i := range tokens {
		if tokens[i].Class == tokenizer.DUMMY {
			continue
		}

		word := &Word{
			Word:     tokens[i].Surface,
			WordType: tokens[i].Features()[0],
		}
		if len(tokens[i].Features()) > 1 {
			word.WordType2 = tokens[i].Features()[1]
		}
		wordMap[tokens[i].Surface] = word
		f := feature{
			WordType: tokens[i].Features()[0],
		}
		if len(tokens[i].Features()) > 1 {
			f.WordType2 = tokens[i].Features()[1]
		}
		features = append(features, f)
	}

	return wordMap, features
}

func initializeData() error {
	japaneseTokenizer := tokenizer.New()
	client := http.DefaultClient

	wordsSyncMap := &sync.Map{}
	featuresSyncMap := &sync.Map{}

	var prevPostsCount int
	err := db.Get(&prevPostsCount, "SELECT count FROM posts_count ORDER BY id DESC LIMIT 1")
	if errors.Is(err, sql.ErrNoRows) {
		prevPostsCount = 0
	} else if err != nil {
		return fmt.Errorf("failed to get previous posts count: %w", err)
	}

	if prevPostsCount >= postsCount {
		log.Println("already initialized")
		return nil
	}

	postStart := prevPostsCount + 1

	wg := sync.WaitGroup{}
	for i := postStart; i <= postsCount; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			title := scrape(client, i)
			wordMap, features := tokenize(title, &japaneseTokenizer)
			for key, word := range wordMap {
				wordsSyncMap.Store(key, word)
			}
			featuresSyncMap.Store(i, features)
		}()
	}

	wg.Wait()

	words := make([]Word, 0, 5*postsCount)
	wordsSyncMap.Range(func(key, value interface{}) bool {
		words = append(words, *(value.(*Word)))
		return true
	})

	wordTypes := make([][]feature, 0, postsCount)
	featuresSyncMap.Range(func(key, value interface{}) bool {
		wordTypes = append(wordTypes, value.([]feature))
		return true
	})

	_, err = db.NamedExec("INSERT INTO words (word, word_type, word_type2) VALUES (:word, :word_type, :word_type2)", words)
	if err != nil {
		return fmt.Errorf("failed to insert words: %w", err)
	}

	sentenceTypes := make([]SentenceType, 0, len(wordTypes))
	sentenceTypeStrMap := make(map[string]struct{}, len(wordTypes))
	for i := range wordTypes {
		if len(wordTypes[i]) == 0 {
			continue
		}
		sentenceTypeStr := ""
		for j := range wordTypes[i] {
			sentenceTypeStr += fmt.Sprintf("(%s|%s),", wordTypes[i][j].WordType, wordTypes[i][j].WordType2)
		}
		if _, ok := sentenceTypeStrMap[sentenceTypeStr]; ok {
			continue
		}
		sentenceTypes = append(sentenceTypes, SentenceType{sentenceTypeStr})
		sentenceTypeStrMap[sentenceTypeStr] = struct{}{}
	}

	_, err = db.NamedExec("INSERT INTO sentences (word_types) VALUES (:word_types)", sentenceTypes)
	if err != nil {
		return fmt.Errorf("failed to insert sentences: %w", err)
	}

	_, err = db.Exec("INSERT INTO posts_count (count) VALUES (?)", postsCount)
	if err != nil {
		return fmt.Errorf("failed to insert posts count: %w", err)
	}

	return nil
}
