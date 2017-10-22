package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"runtime"
	"strings"

	"github.com/birkelund/boltdbcache"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/joho/godotenv"
	"github.com/k0kubun/pp"
	"github.com/peterbourgon/diskv"
	"github.com/roscopecoltran/mxj"
	// "github.com/roscopecoltran/configor"
)

var transportCache *httpcache.Transport

//var cache *httpcache.Cache

type Handler struct {
	Disabled bool `default:"false" help:"Disable handler init" json:"disabled,omitempty" yaml:"disabled,omitempty" toml:"disabled,omitempty"`

	Env     EnvConfig         `opts:"-" json:"env,omitempty" yaml:"env,omitempty" toml:"env,omitempty"`
	Etcd    EtcdConfig        `opts:"-" json:"etcd,omitempty" yaml:"etcd,omitempty" toml:"etcd,omitempty"`
	Config  Config            `opts:"-" json:"config,omitempty" yaml:"config,omitempty" toml:"config,omitempty"`
	Headers map[string]string `opts:"-" json:"headers,omitempty" yaml:"headers,omitempty" toml:"headers,omitempty"`
	Auth    string            `help:"Basic auth credentials <user>:<pass>" json:"auth,omitempty" yaml:"auth,omitempty" toml:"auth,omitempty"`
	Log     bool              `default:"false" opts:"-" json:"log,omitempty" yaml:"log,omitempty" toml:"log,omitempty"`
	Debug   bool              `default:"false" help:"Enable debug output" json:"debug,omitempty" yaml:"debug,omitempty" toml:"debug,omitempty"`
	// transport *httpcache.Transport `opts:"-" json:"-" yaml:"-" toml:"-"`
}

func (h *Handler) LoadConfigFile(path string) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	return h.LoadConfig(b)
}

func (h *Handler) GetConfigPaths(path string) []string {
	var paths []string
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return paths
	}
	mxj.JsonUseNumber = true
	mv, err := mxj.NewMapJson(b)
	if err != nil {
		fmt.Println("NewMapJson, error: ", err)
	}
	fmt.Println("NewMapJson, jdata:", string(b))
	fmt.Printf("NewMapJson, mv: \n %#v\n", mv)
	mxj.LeafUseDotNotation()
	paths = mv.LeafPaths()
	return paths
}

var Endpoints struct {
	Disabled bool
	Routes   []string
}

func (h *Handler) LoadConfig(b []byte) error {
	c := Config{}

	if err := json.Unmarshal(b, &c); err != nil { //json unmarshal performs selector validation
		return err
	}

	h.Etcd = c.Etcd
	if len(c.Env.Files) > 0 {
		envVars, err := godotenv.Read(c.Env.Files...)
		if err != nil {
			return err
		}
		c.Env.VariablesList = envVars
		envVarsTree := make(map[string]map[string]string)
		for k, v := range envVars {
			var varParentKey, varChildrenKey string
			varParts := strings.Split(k, "_")
			if len(varParts) > 1 {
				varParentKey = varParts[0]
				varChildrenKey = strings.Join(varParts[1:], "_")
			}
			if v != "" && varParentKey != "" && varChildrenKey != "" {
				envVarsTree[varParentKey] = make(map[string]string)
				envVarsTree[varParentKey][varChildrenKey] = v
			}
		}
		c.Env.VariablesTree = envVarsTree
	}

	if h.Log {
		for k, e := range c.Routes {
			// Ovveride value ?! which cases ?!
			// e.Debug = h.Debug
			if strings.HasPrefix(e.Route, "/") {
				e.Route = strings.TrimPrefix(e.Route, "/")
				c.Routes[k] = e
			}

			if h.Debug {
				logf("Loaded endpoint: /%s", e.Route)
			}
			Endpoints.Routes = append(Endpoints.Routes, e.Route)
			if len(h.Headers) > 0 && h.Debug { // Copy the Header attributes (only if they are not yet set)
				fmt.Printf("h.Headers, len=%d:\n", len(h.Headers))
				pp.Println(h.Headers)
			}
			for k, v := range e.HeadersJSON {
				if len(e.HeadersJSON) > 0 && h.Debug {
					pp.Println("header key: ", k)
					pp.Println("header val: ", v)
				}
				for kl, vl := range c.Env.VariablesList {
					holderKey := fmt.Sprintf("{{%s}}", strings.Replace(kl, "\"", "", -1))
					v = strings.Replace(v, holderKey, vl, -1)
				}
				e.HeadersJSON[k] = strings.Trim(v, " ")
			}
			if e.HeadersJSON == nil {
				e.HeadersJSON = h.Headers
			} else {
				for k, v := range h.Headers {
					if _, ok := e.HeadersJSON[k]; !ok {
						e.HeadersJSON[k] = v
					}
				}
			}
			if len(e.HeadersJSON) > 0 && h.Debug {
				fmt.Printf("e.HeadersJSON, len=%d:\n", len(e.HeadersJSON))
				pp.Println(e.HeadersJSON)
			}
		}
	}
	if h.Debug {
		logf("Enabled debug mode")
	}

	// c.Transport =
	// InitCache("./shared/cache/test.boltdb")
	InitCache("./shared/cache")

	h.Config = c //replace config
	return nil
}

func InitCache(cachePath string) {
	runtime.GOMAXPROCS(runtime.NumCPU())
	transportCache = newTransportWithDiskCache(cachePath, "")
}

func newTransportWithDiskCache(basePath string, engine string) *httpcache.Transport {
	// var cache *httpcache.Cache
	if engine == "boltdbcache" {
		cache, err := boltdbcache.New(basePath)
		if err != nil {
			fmt.Println("error: ", err)
		}
		// cache := boltdbcache.NewWithDB(d)
		return httpcache.NewTransport(cache)
	} else {
		d := diskv.New(diskv.Options{
			BasePath:     basePath,
			CacheSizeMax: 500 * 1024 * 250, // 10MB
		})
		cache := diskcache.NewWithDiskv(d)
		return httpcache.NewTransport(cache)
	}
	//pp.Println(cache)
	//return httpcache.NewTransport(cache)
}

func getClient() *http.Client {
	c := transportCache.Client()
	// c.Timeout = time.Duration(30 * time.Second) //TODO Client Transport of type *httpcache.Transport doesn't support CanelRequest; Timeout not supported
	return c
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// basic auth
	if h.Auth != "" {
		u, p, _ := r.BasicAuth()
		if h.Auth != u+":"+p {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Access Denied"))
			return
		}
	}
	// always JSON!
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// admin actions
	if r.URL.Path == "" || r.URL.Path == "/" {
		get := false
		if r.Method == "GET" {
			get = true
		} else if r.Method == "POST" {
			b, err := ioutil.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write(jsonerr(err))
				return
			}
			if err := h.LoadConfig(b); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write(jsonerr(err))
				return
			}
			get = true
		}
		if !get {
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write(jsonerr(errors.New("Use GET or POST")))
			return
		}
		b, _ := json.MarshalIndent(h.Config, "", "  ")
		w.Write(b)
		return
	}
	// endpoint id (excludes root slash)
	id := r.URL.Path[1:]
	// load endpoint
	endpoint := h.Endpoint(id)
	if endpoint == nil {
		w.WriteHeader(404)
		w.Write(jsonerr(fmt.Errorf("Endpoint /%s not found", id)))
		return
	}
	// convert url.Values into map[string]string
	values := map[string]string{}
	for k, v := range r.URL.Query() {
		values[k] = v[0]
	}
	var err error
	res := make(map[string][]Result, 0)
	if h.Debug {
		pp.Printf("endpoint.Concurrency: %s \n", endpoint.Concurrency)
	}
	if endpoint.Concurrency >= 1 && len(endpoint.Pager["max"]) > 0 {
		ctx := context.Background()
		resChan := make(chan *ScraperResult, endpoint.Concurrency)
		go endpoint.ExecuteParallel(ctx, values, resChan)
		totalResults, totalErrors := 0, 0
		for endpointResult := range resChan {
			if endpointResult.Error == nil {
				for k, v := range endpointResult.List {
					if _, ok := res[k]; !ok {
						res[k] = make([]Result, 0)
					}
					for _, r := range v {
						res[k] = append(res[k], r)
					}
					totalResults = totalResults + len(v)
				}
				if h.Debug {
					fmt.Printf("res length: %d", len(res))
				}
			} else {
				totalErrors++
			}
		}
		if h.Debug {
			fmt.Printf("totalResults: %d/%d, totalErrors: %d \n", totalResults, len(res["result"]), totalErrors)
		}

	} else {
		res, err = endpoint.Execute(values)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(jsonerr(err))
			return
		}
	}

	// encode as JSON
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(res); err != nil {
		w.Write([]byte("JSON Error: " + err.Error()))
	}

	/*
		var v interface{}
		if endpoint.List == "" && len(res) == 1 {
			v = res[0]
		} else {
			v = res
		}
		if err := enc.Encode(v); err != nil {
			w.Write([]byte("JSON Error: " + err.Error()))
		}
	*/

}

// Endpoint will return the Handler's Endpoint from its Config
func (h *Handler) Endpoint(path string) *Endpoint {
	var keyCfg int
	for k, v := range h.Config.Routes {
		if v.Route == path {
			keyCfg = k
			break
		}
	}
	if h.Config.Routes[keyCfg] != nil {
		return h.Config.Routes[keyCfg]
	}
	return nil
}
