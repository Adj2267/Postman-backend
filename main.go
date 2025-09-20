package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var apiKey string
var httpClient = &http.Client{Timeout: 10 * time.Second}

type searchItem struct {
	Title  string `json:"Title"`
	ImdbID string `json:"imdbID"`
	Type   string `json:"Type"`
}
type searchResult struct {
	Search   []searchItem `json:"Search"`
	Response string       `json:"Response"`
	Error    string       `json:"Error"`
}

func main() {
	_ = godotenv.Load()
	apiKey = os.Getenv("OMDB_API_KEY")
	if apiKey == "" {
		fmt.Println("OMDB_API_KEY missing in .env")
		return
	}
	r := gin.Default()
	r.GET("/api/movie", movieHandler)
	r.GET("/api/episode", episodeHandler)
	r.GET("/api/movies/genre", moviesByGenreHandler)
	r.GET("/api/recommend", recommendHandler)
	r.Run(":8080")
}

func omdbURL(params map[string]string) string {
	v := url.Values{}
	v.Set("apikey", apiKey)
	for k, val := range params {
		v.Set(k, val)
	}
	return "https://www.omdbapi.com/?" + v.Encode()
}

func fetchJSON(u string, out interface{}) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "go-movie-api/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func movieHandler(c *gin.Context) {
	t := c.Query("title")
	if t == "" {
		c.JSON(400, gin.H{"error": "missing title"})
		return
	}
	u := omdbURL(map[string]string{"t": t, "plot": "full"})
	var m map[string]interface{}
	if err := fetchJSON(u, &m); err != nil || m["Response"] == "False" {
		c.JSON(404, gin.H{"error": "movie not found"})
		return
	}
	c.JSON(200, gin.H{
		"Title":    m["Title"],
		"Year":     m["Year"],
		"Plot":     m["Plot"],
		"Country":  m["Country"],
		"Awards":   m["Awards"],
		"Director": m["Director"],
		"Ratings":  m["Ratings"],
	})
}

func episodeHandler(c *gin.Context) {
	s := c.Query("series_title")
	se := c.Query("season")
	e := c.Query("episode_number")
	if s == "" || se == "" || e == "" {
		c.JSON(400, gin.H{"error": "missing parameters"})
		return
	}
	u := omdbURL(map[string]string{"t": s, "Season": se, "Episode": e, "plot": "full"})
	var m map[string]interface{}
	if err := fetchJSON(u, &m); err != nil || m["Response"] == "False" {
		c.JSON(404, gin.H{"error": "episode not found"})
		return
	}
	c.JSON(200, gin.H{
		"Title":      m["Title"],
		"Season":     m["Season"],
		"Episode":    m["Episode"],
		"Released":   m["Released"],
		"Plot":       m["Plot"],
		"imdbRating": m["imdbRating"],
	})
}

func searchByKeyword(keyword string, page int) []searchItem {
	u := omdbURL(map[string]string{"s": keyword, "page": strconv.Itoa(page)})
	var sr searchResult
	if err := fetchJSON(u, &sr); err != nil || sr.Response == "False" {
		return nil
	}
	return sr.Search
}

func getDetailByID(id string) (map[string]interface{}, error) {
	u := omdbURL(map[string]string{"i": id, "plot": "short"})
	var md map[string]interface{}
	if err := fetchJSON(u, &md); err != nil || md["Response"] == "False" {
		return nil, fmt.Errorf("not found")
	}
	return md, nil
}

func getDetailByTitle(title string) (map[string]interface{}, error) {
	u := omdbURL(map[string]string{"t": title, "plot": "short"})
	var md map[string]interface{}
	if err := fetchJSON(u, &md); err == nil {
		if md["Response"] == "True" {
			return md, nil
		}
	}
	for p := 1; p <= 2; p++ {
		items := searchByKeyword(title, p)
		if items == nil {
			continue
		}
		for _, it := range items {
			if it.ImdbID == "" {
				continue
			}
			if m, err := getDetailByID(it.ImdbID); err == nil {
				return m, nil
			}
		}
	}
	return nil, fmt.Errorf("not found")
}

func collectByGenre(gen string, limit int) []map[string]interface{} {
	found := map[string]map[string]interface{}{}
	kw := []string{"the", "a", "man", "love", "star", "dark", "king", "matrix", "avengers"}
	for _, k := range kw {
		items := searchByKeyword(k, 1)
		if items == nil {
			continue
		}
		for _, it := range items {
			if it.ImdbID == "" {
				continue
			}
			if _, ok := found[it.ImdbID]; ok {
				continue
			}
			md, err := getDetailByID(it.ImdbID)
			if err != nil {
				continue
			}
			if g, ok := md["Genre"].(string); ok && strings.Contains(strings.ToLower(g), strings.ToLower(gen)) {
				found[it.ImdbID] = md
				if len(found) >= limit {
					break
				}
			}
		}
		if len(found) >= limit {
			break
		}
	}
	out := make([]map[string]interface{}, 0, len(found))
	for _, v := range found {
		out = append(out, v)
	}
	return out
}

func ratingVal(m map[string]interface{}) float64 {
	if r, ok := m["imdbRating"].(string); ok && r != "N/A" && r != "" {
		if f, err := strconv.ParseFloat(r, 64); err == nil {
			return f
		}
	}
	return 0
}

func topByRating(list []map[string]interface{}, n int) []map[string]interface{} {
	sort.Slice(list, func(i, j int) bool { return ratingVal(list[i]) > ratingVal(list[j]) })
	if len(list) > n {
		return list[:n]
	}
	return list
}

func moviesByGenreHandler(c *gin.Context) {
	genre := c.Query("genre")
	if genre == "" {
		c.JSON(400, gin.H{"error": "missing genre"})
		return
	}
	cands := collectByGenre(genre, 150)
	top := topByRating(cands, 15)
	out := make([]gin.H, 0, len(top))
	for _, m := range top {
		out = append(out, gin.H{
			"Title":      m["Title"],
			"Year":       m["Year"],
			"imdbID":     m["imdbID"],
			"Genre":      m["Genre"],
			"imdbRating": m["imdbRating"],
		})
	}
	c.JSON(200, gin.H{"genre": genre, "count": len(out), "movies": out})
}

func recommendHandler(c *gin.Context) {
	fav := c.Query("favorite_movie")
	if fav == "" {
		c.JSON(400, gin.H{"error": "missing favorite_movie"})
		return
	}
	seed, err := getDetailByTitle(fav)
	if err != nil {
		c.JSON(404, gin.H{"error": "favorite movie not found"})
		return
	}
	perLevel := 20
	seen := map[string]bool{}
	if id, ok := seed["imdbID"].(string); ok && id != "" {
		seen[id] = true
	}
	result := []map[string]interface{}{}
	if g, ok := seed["Genre"].(string); ok {
		for _, gg := range strings.Split(g, ",") {
			gg = strings.TrimSpace(gg)
			cands := topByRating(collectByGenre(gg, perLevel), perLevel)
			for _, m := range cands {
				if id, ok := m["imdbID"].(string); ok && !seen[id] {
					seen[id] = true
					result = append(result, m)
					if len(result) >= perLevel {
						break
					}
				}
			}
			if len(result) >= perLevel {
				break
			}
		}
	}
	if len(result) < perLevel {
		if d, ok := seed["Director"].(string); ok {
			for _, dir := range strings.Split(d, ",") {
				dir = strings.TrimSpace(dir)
				cands := topByRating(collectByGenre(dir, perLevel), perLevel) // small fallback: genre-like by director name search
				for _, m := range cands {
					if id, ok := m["imdbID"].(string); ok && !seen[id] {
						seen[id] = true
						result = append(result, m)
						if len(result) >= perLevel {
							break
						}
					}
				}
				if len(result) >= perLevel {
					break
				}
			}
		}
	}
	if len(result) < perLevel {
		if a, ok := seed["Actors"].(string); ok {
			for _, actor := range strings.Split(a, ",") {
				actor = strings.TrimSpace(actor)
				cands := topByRating(collectByGenre(actor, perLevel), perLevel) // fallback
				for _, m := range cands {
					if id, ok := m["imdbID"].(string); ok && !seen[id] {
						seen[id] = true
						result = append(result, m)
						if len(result) >= perLevel {
							break
						}
					}
				}
				if len(result) >= perLevel {
					break
				}
			}
		}
	}
	if len(result) > perLevel {
		result = result[:perLevel]
	}
	out := make([]gin.H, 0, len(result))
	for _, m := range result {
		out = append(out, gin.H{
			"Title":      m["Title"],
			"Year":       m["Year"],
			"imdbID":     m["imdbID"],
			"Genre":      m["Genre"],
			"Director":   m["Director"],
			"Actors":     m["Actors"],
			"imdbRating": m["imdbRating"],
		})
	}
	c.JSON(200, gin.H{"favorite_movie": seed["Title"], "recommendations": out})
}
