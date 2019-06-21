package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/rs/cors"

	terraConfig "github.com/osallou/goterra-lib/lib/config"
	terraDb "github.com/osallou/goterra-lib/lib/db"
	terraToken "github.com/osallou/goterra-lib/lib/token"
	terraUser "github.com/osallou/goterra-lib/lib/user"
)

// Version of server
var Version string

// HomeHandler manages base entrypoint
var HomeHandler = func(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{"version": Version, "message": "ok"}
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// DeploymentData represents data sent to update a deployment value
type DeploymentData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Claims contains JWT claims
type Claims struct {
	// Deployment string          `json:"deployment"`
	UID       string          `json:"uid"`
	Admin     bool            `json:"admin"`
	UserNS    map[string]bool `json:"userns"` // map of namespace names, if true user is owner of namespace else only a member
	Namespace string          `json:"namespace"`
	jwt.StandardClaims
}

// CheckAPIKey check X-API-Key authorization content and returns user info
func CheckAPIKey(apiKey string) (data terraUser.AuthData, err error) {
	err = nil
	if apiKey == "" {
		return data, errors.New("no api key provided")
	}

	data = terraUser.AuthData{}
	if os.Getenv("GOT_FEAT_ANONYMOUS") == "1" {
		user := terraUser.User{UID: apiKey, Logged: true}
		userJSON, _ := json.Marshal(user)
		data.User = user
		token, tokenErr := terraToken.FernetEncode(userJSON)
		if tokenErr != nil {
			err = errors.New("failed to create token")
		} else {
			data.Token = string(token)
		}
	} else {
		var tauthErr error
		data, tauthErr = terraUser.Check(apiKey)

		if tauthErr != nil {
			err = fmt.Errorf("invalid api key: %s", tauthErr)
			return data, err
		}
		data.User.Logged = true

	}
	log.Printf("[DEBUG] User logged: %s", data.User.UID)
	return data, err
}

// checkAPIKeyAdminOrOwner checks that api key is valid and user is admin or owner of deployment
func checkAPIKeyAdminOrOwner(apikey string, deployment string) bool {
	isAdminOrOwner := false
	if apikey != "" && deployment != "" {
		data, err := CheckAPIKey(apikey)
		if err == nil {
			user := data.User
			if user.Admin {
				isAdminOrOwner = true
				return isAdminOrOwner
			}
			config := terraConfig.LoadConfig()
			dbHandler := terraDb.NewClient(config)
			value, err := dbHandler.Client.HGet(dbHandler.Prefix+":depl:"+deployment, "user").Result()
			if err != nil && value == user.UID {
				isAdminOrOwner = true
				return isAdminOrOwner
			}
		}
	}
	return isAdminOrOwner
}

// CheckTokenForDeployment checks token and token user maps to current deployment owner
func CheckTokenForDeployment(authToken string, deployment string) bool {
	// config := terraConfig.LoadConfig()

	tokenStr := strings.Replace(authToken, "Bearer", "", -1)
	tokenStr = strings.TrimSpace(tokenStr)

	userJSON, err := terraToken.FernetDecode([]byte(tokenStr))
	if err != nil {
		fmt.Printf("Token error: %v\n", err)
		return false
	}

	user := terraUser.User{}
	json.Unmarshal(userJSON, &user)
	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	value, err := dbHandler.Client.HGet(dbHandler.Prefix+":depl:"+deployment, "user").Result()
	if err == nil && value == user.UID {
		return true
	}
	return false

}

// DeploymentHandler creates a deployment
var DeploymentHandler = func(w http.ResponseWriter, r *http.Request) {
	data, err := CheckAPIKey(r.Header.Get("X-API-Key"))
	user := data.User
	token := data.Token
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		respError := map[string]interface{}{"message": fmt.Sprintf("Auth error: %s", err)}
		json.NewEncoder(w).Encode(respError)
		return
	}
	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	id := uuid.New()
	idStr := id.String()
	t := time.Now()
	dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+idStr, "user", user.UID).Err()
	if r.Header.Get("X-API-NS") != "" {
		dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+idStr, "ns", r.Header.Get("X-API-NS")).Err()
	}
	err = dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+idStr, "ts", t.Unix()).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		respError := map[string]interface{}{"message": "failed to generate deployment id"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"url": config.URL, "id": idStr, "token": token}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentDeleteHandler deletes a deployment info
var DeploymentDeleteHandler = func(w http.ResponseWriter, r *http.Request) {
	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)

	vars := mux.Vars(r)

	isAdminOrOwner := checkAPIKeyAdminOrOwner(r.Header.Get("X-API-Key"), vars["deployment"])

	if !isAdminOrOwner && !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	idStr := vars["deployment"]
	err := dbHandler.Client.Del(dbHandler.Prefix + ":depl:" + idStr).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		respError := map[string]interface{}{"message": "failed to delete deployment id"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	// w.WriteHeader(http.StatusNotFound)
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"message": "deleted"}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentUpdateHandler updates a deployment
var DeploymentUpdateHandler = func(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	data := &DeploymentData{}
	err := json.NewDecoder(r.Body).Decode(data)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "failed to decode message"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	isAdminOrOwner := checkAPIKeyAdminOrOwner(r.Header.Get("X-API-Key"), vars["deployment"])

	if !isAdminOrOwner && !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	t := time.Now()
	err = dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+vars["deployment"], "lastts", t.Unix()).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "failed to update deployment"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	err = dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+vars["deployment"], data.Key, data.Value).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "failed to update deployment"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	// w.WriteHeader(http.StatusNotFound)
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"message": "done"}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentGetHandler returns a deployment value, not found if not defined
var DeploymentGetHandler = func(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	isAdminOrOwner := checkAPIKeyAdminOrOwner(r.Header.Get("X-API-Key"), vars["deployment"])

	if !isAdminOrOwner && !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	value, err := dbHandler.Client.HGet(dbHandler.Prefix+":depl:"+vars["deployment"], vars["key"]).Result()
	if err != nil || value == "" {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not found"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"key": vars["key"], "value": value}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentGetKeysHandler returns available variables for deployment
var DeploymentGetKeysHandler = func(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	isAdminOrOwner := checkAPIKeyAdminOrOwner(r.Header.Get("X-API-Key"), vars["deployment"])

	if !isAdminOrOwner && !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	values, err := dbHandler.Client.HGetAll(dbHandler.Prefix + ":depl:" + vars["deployment"]).Result()
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not found"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	// Return only elts starting with filter query param
	filter, ok := r.URL.Query()["filter"]
	if ok && len(filter) == 1 {
		filteredValues := make(map[string]string)
		for key, val := range values {
			if strings.HasPrefix(key, filter[0]) {
				filteredValues[key] = val
			}
		}
		w.Header().Add("Content-Type", "application/json")
		resp := map[string]interface{}{"deployment": filteredValues}
		json.NewEncoder(w).Encode(resp)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"deployment": values}
	json.NewEncoder(w).Encode(resp)
}

func main() {
	var configFilePath string
	flag.StringVar(&configFilePath, "config", os.Getenv("GOT_CONFIG"), "configuration file path")
	flag.Parse()
	terraConfig.ConfigFile = configFilePath

	config := terraConfig.LoadConfig()

	consulErr := terraConfig.ConsulDeclare("got-store", "/store")
	if consulErr != nil {
		fmt.Printf("Failed to register: %s", consulErr.Error())
		panic(consulErr)
	}

	r := mux.NewRouter()
	r.HandleFunc("/store", HomeHandler).Methods("GET")
	r.HandleFunc("/store", DeploymentHandler).Methods("POST")
	r.HandleFunc("/store/{deployment}", DeploymentGetKeysHandler).Methods("GET")
	r.HandleFunc("/store/{deployment}", DeploymentUpdateHandler).Methods("PUT")
	r.HandleFunc("/store/{deployment}", DeploymentGetKeysHandler).Methods("GET")
	r.HandleFunc("/store/{deployment}", DeploymentDeleteHandler).Methods("DELETE")
	r.HandleFunc("/store/{deployment}/{key}", DeploymentGetHandler).Methods("GET")

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
		AllowedHeaders:   []string{"Authorization"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
		Debug:            true,
	})

	handler := c.Handler(r)

	loggedRouter := handlers.LoggingHandler(os.Stdout, handler)

	srv := &http.Server{
		Handler: loggedRouter,
		Addr:    fmt.Sprintf("%s:%d", config.Web.Listen, config.Web.Port),
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())

}
