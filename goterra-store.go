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

	terraConfig "github.com/osallou/goterra-lib/lib/config"
	terraDb "github.com/osallou/goterra-lib/lib/db"
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
	Deployment string          `json:"deployment"`
	UID        string          `json:"uid"`
	Admin      bool            `json:"admin"`
	UserNS     map[string]bool `json:"userns"` // map of namespace names, if true user is owner of namespace else only a member
	Namespace  string          `json:"namespace"`
	jwt.StandardClaims
}

// CheckAPIKey check X-API-Key authorization content and returns user info
func CheckAPIKey(apiKey string) (user terraUser.User, err error) {
	err = nil
	user = terraUser.User{}
	if apiKey == "" {
		if os.Getenv("GOT_FEAT_ANONYMOUS") == "1" {
			user = terraUser.User{UID: "anonymous", Logged: true}
		} else {
			err = errors.New("missing X-API-Key")
		}
	} else {
		user, tauthErr := terraUser.Check(apiKey)
		if tauthErr != nil {
			err = errors.New("invalid api key")
		} else {
			user.Logged = true
		}
	}
	log.Printf("[DEBUG] User logged: %s", user.UID)
	return user, err
}

// CheckTokenForDeployment checks JWT token and token maps to current deployment
func CheckTokenForDeployment(authToken string, deployment string) bool {
	config := terraConfig.LoadConfig()

	tokenStr := strings.Replace(authToken, "Bearer", "", -1)
	tokenStr = strings.TrimSpace(tokenStr)
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.Secret), nil
	})
	if err != nil || !token.Valid || claims.Audience != "goterra/deployment" {
		fmt.Printf("Token error: %v\n", err)
		return false
	}
	if !claims.Admin {
		if claims.Deployment != deployment {
			fmt.Printf("Trying to access a different deployment %s from %s\n", deployment, claims.Deployment)
			return false
		}
	}
	return true
}

// DeploymentHandler creates a deployment
var DeploymentHandler = func(w http.ResponseWriter, r *http.Request) {
	user, err := CheckAPIKey(r.Header.Get("X-API-Key"))
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
	mySigningKey := []byte(config.Secret)

	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		Deployment: idStr,
		UID:        user.UID,
		Admin:      user.Admin,
		// UserNS:     user.Namespaces,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
			Audience:  "goterra/deployment",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(mySigningKey)
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"url": config.URL, "id": idStr, "token": tokenString}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentDeleteHandler deletes a deployment info
var DeploymentDeleteHandler = func(w http.ResponseWriter, r *http.Request) {
	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)

	vars := mux.Vars(r)

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
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

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
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

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
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

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
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
	r.HandleFunc("/store/{deployment}", DeploymentUpdateHandler).Methods("PUT")
	r.HandleFunc("/store/{deployment}", DeploymentGetKeysHandler).Methods("GET")
	r.HandleFunc("/store/{deployment}", DeploymentDeleteHandler).Methods("DELETE")
	r.HandleFunc("/store/{deployment}/{key}", DeploymentGetHandler).Methods("GET")

	loggedRouter := handlers.LoggingHandler(os.Stdout, r)

	srv := &http.Server{
		Handler: loggedRouter,
		Addr:    fmt.Sprintf("%s:%d", config.Web.Listen, config.Web.Port),
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())

}
