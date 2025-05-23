package web

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"code.dogecoin.org/dkm/internal"
	"code.dogecoin.org/dkm/internal/keymgr"
	"code.dogecoin.org/governor"
	"github.com/dogeorg/doge"
	"github.com/dogeorg/doge/bip39"
)

type WebAPI struct {
	governor.ServiceCtx
	store  internal.Store
	cstore internal.StoreCtx
	keymgr internal.KeyMgr
	srv    http.Server
}

func New(bind internal.Address, store internal.Store, keymgr internal.KeyMgr) governor.Service {
	mux := http.NewServeMux()
	a := &WebAPI{
		store:  store,
		keymgr: keymgr,
		srv: http.Server{
			Addr:    bind.String(),
			Handler: mux,
		},
	}
	mux.HandleFunc("/create", a.create)
	mux.HandleFunc("/login", a.login)
	mux.HandleFunc("/roll-token", a.rollToken)
	mux.HandleFunc("/logout", a.logout)
	mux.HandleFunc("/change-password", a.changePassword)
	mux.HandleFunc("/recover-password", a.recoverPassword)
	mux.HandleFunc("/create-delegate", a.createDelegate)
	mux.HandleFunc("/get-delegate-key", a.getDelegatePriv)
	mux.HandleFunc("/get-delegate-pub", a.getDelegatePub)
	mux.HandleFunc("/make-delegate", a.makeDelegate)

	return a
}

// goroutine
func (a *WebAPI) Run() {
	a.cstore = a.store.WithCtx(a.Context) // Service Context is first available here
	log.Printf("[dkm] listening on: %v", a.srv.Addr)
	if err := a.srv.ListenAndServe(); err != http.ErrServerClosed { // blocking call
		log.Printf("[dkm] HTTP server: %v", err)
	}
}

// called on any
func (a *WebAPI) Stop() {
	// new goroutine because Shutdown() blocks
	go func() {
		// cannot use ServiceCtx here because it's already cancelled
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		a.srv.Shutdown(ctx) // blocking call
		cancel()
	}()
}

// WEB API

type CreateRequest struct {
	Password string `json:"password"`
}
type CreateResponse struct {
	Seedphrase []string `json:"seedphrase"`
}

// API: /create {"password":"xyz"}
// => {"seedphrase":["remain","nothing","vendor", (24 words) ]}
// => {"error":"password","reason":"password is empty"}
func (a *WebAPI) create(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args CreateRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// validate password
		pass := strings.TrimSpace(args.Password)
		if len(pass) < 1 {
			sendError(w, http.StatusInternalServerError, "password", "password is empty", options)
			return
		}

		// generate the new key
		mnemonic, err := a.keymgr.CreateKey(pass)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
		}

		// response
		res := CreateResponse{Seedphrase: mnemonic}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type LoginRequest struct {
	Password string `json:"password"`
}
type LoginResponse struct {
	Token    string `json:"token"`
	ValidFor int    `json:"valid_for"`
}

// API: /login {"password":"xyz"}
// => {"token":"652b2b63ca6273119b0deb1da807879e","valid_for":600}
// => {"error":"password","reason":"incorrect password"}
func (a *WebAPI) login(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args LoginRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// validate password
		pass := strings.TrimSpace(args.Password)
		if len(pass) < 1 {
			sendError(w, http.StatusInternalServerError, "password", "password is empty", options)
			return
		}

		token, ends, err := a.keymgr.LogIn(pass)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
		}

		// response
		res := LoginResponse{Token: token, ValidFor: ends}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type RollTokenRequest struct {
	Token string `json:"token"`
}
type RollTokenResponse struct {
	Token    string `json:"token"`
	ValidFor int    `json:"valid_for"`
}

// API: /roll-token {"token":"652b2b63ca6273119b0deb1da807879e"}
// => {"token":"52eef94ed16ea8dd1412c982d91e7de4","valid_for":600}
// => {"error":"token","reason":"invalid or expired token"}
func (a *WebAPI) rollToken(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args RollTokenRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		newtoken, ends, err := a.keymgr.RollToken(args.Token)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
			return
		}

		// response
		res := RollTokenResponse{Token: newtoken, ValidFor: ends}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type LogOutRequest struct {
	Token string `json:"token"`
}
type LogOutResponse struct {
}

// API: /logout {"token":"39d5c614a1c1bf4e7d117d0287d6dc41"}
// => {}
func (a *WebAPI) logout(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args LogOutRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		a.keymgr.LogOut(args.Token)

		// response
		res := LogOutResponse{}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type ChangePassRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	Seedphrase      string `json:"seedphrase"`
}
type ChangePassResponse struct {
	Changed bool `json:"changed"`
}

// API: /change-password {"current_password":"xya","new_password":"xyz"} or {"seedphrase":["word1","word2","word3"],"new_password":"xyz"} or {"seedphrase":"word1,word2,word3","new_password":"xyz"}
// => {"changed":true}
// => {"error":"password","reason":"incorrect password"}
// => {"error":"password","reason":"password is empty"}
// => {"error":"newpassword","reason":"new password is empty"}
// => {"error":"nokey","reason":"key has not been created"}
// => {"error":"length","reason":"wrong mnemonic length: must be 12, 15, 18, 21 or 24 words"}
// => {"error":"wordlist","reason":"wrong word in mnemonic phrase: not on the wordlist"}
// => {"error":"checksum","reason":"wrong mnemonic phrase: checksum doesn't match"}
func (a *WebAPI) changePassword(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"

	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args ChangePassRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// validate new password
		newpass := strings.TrimSpace(args.NewPassword)
		if len(newpass) < 1 {
			sendError(w, http.StatusInternalServerError, "newpassword", "new password is empty", options)
			return
		}

		// validate either password or seedphrase is provided
		pass := strings.TrimSpace(args.CurrentPassword)
		hasPass := len(pass) > 0
		hasSeed := len(args.Seedphrase) > 0

		if !hasPass && !hasSeed {
			sendError(w, http.StatusInternalServerError, "auth", "either current_password or seedphrase must be provided", options)
			return
		}

		if hasPass {
			err = a.keymgr.ChangePassword(pass, newpass)
			if err != nil {
				if errors.Is(err, keymgr.ErrWrongPassword) {
					sendError(w, http.StatusForbidden, "password", "password", options)
				} else {
					sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
				}
				return
			}
		} else {
			// Handle seedphrase as a string with flexible separators
			seedString := strings.TrimSpace(args.Seedphrase)
			// Replace all commas with spaces
			seedString = strings.ReplaceAll(seedString, ",", " ")
			// Split on any whitespace (handles multiple spaces)
			seedphrase := strings.Fields(seedString)

			err = a.keymgr.RecoverPassword(seedphrase, newpass)
			if err != nil {
				if errors.Is(err, keymgr.ErrWrongMnemonic) {
					log.Printf("[dkm] change-password: seedphrase validation failed - mnemonic does not match existing key")
				}
				sendError(w, http.StatusInternalServerError, "seedphrase", "seedphrase", options)
				return
			}
		}

		// response
		res := ChangePassResponse{Changed: true}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type RecoveryRequest struct {
	Seedphrase  []string `json:"seedphrase"`
	NewPassword string   `json:"new_password"`
}
type RecoveryResponse struct {
	Changed bool `json:"changed"`
}

// API: /recover-password {"seedphrase":["scare","fury", (24 words) ],"new_password":"xyz"}
// => {"changed":true}
// => {"error":"length","reason":"wrong mnemonic length: must be 12, 15, 18, 21 or 24 words"}
// => {"error":"wordlist","reason":"wrong word in mnemonic phrase: not on the wordlist"}
// => {"error":"checksum","reason":"wrong mnemonic phrase: checksum doesn't match"}
// => {"error":"seedphrase","reason":"missing seedphrase"}
// => {"error":"newpassword","reason":"new password is empty"}
// => {"error":"nokey","reason":"key has not been created"}
func (a *WebAPI) recoverPassword(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args RecoveryRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// validate new password
		newpass := strings.TrimSpace(args.NewPassword)
		if len(newpass) < 1 {
			sendError(w, http.StatusInternalServerError, "newpassword", "new password is empty", options)
			return
		}
		if len(args.Seedphrase) < 1 {
			sendError(w, http.StatusInternalServerError, "seedphrase", "missing seedphrase", options)
			return
		}

		// attempt to change the password
		err = a.keymgr.RecoverPassword(args.Seedphrase, newpass)
		if err != nil {
			if errors.Is(err, keymgr.ErrWrongMnemonic) {
				sendError(w, http.StatusForbidden, "password", "password", options)
			} else {
				sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
			}
			return
		}

		// response
		res := RecoveryResponse{Changed: true}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type CreateDelegateRequest struct {
	ID   string `json:"id"`
	Pass string `json:"password"`
}
type CreateDelegateResponse struct {
	Token string `json:"token"`
	Pub   string `json:"pub"`
}

// API: /create-delegate { id:"pup.xyz", password:"dogebox-rulez" }
//
// Success: { token:"hex", pub:"hex" }
// Failure: { error:"bad-request|entropy|exists|password|nokey|error", "reason":"str" }
//
// Errors:
//
//	entropy: insufficient entropy available
//	exists: delegate key for this id already exists
//	password: wrong password for master key
//	nokey: master key hasn't been created yet
//	error: system nonsense
func (a *WebAPI) createDelegate(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args CreateDelegateRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}
		if args.ID == "" {
			sendError(w, http.StatusInternalServerError, "bad-request", "missing 'id'", options)
			return
		}
		if args.Pass == "" {
			sendError(w, http.StatusInternalServerError, "bad-request", "missing 'password'", options)
			return
		}

		token, pub, err := a.keymgr.CreateDelegate(args.ID, args.Pass)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
		}

		// response
		res := CreateDelegateResponse{Token: token, Pub: hex.EncodeToString(pub)}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type MakeDelegateRequest struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}
type MakeDelegateResponse struct {
	Pub  string `json:"pub"`
	Priv string `json:"priv"`
	Wif  string `json:"wif"`
}

// API: /make-delegate { id:"pup.xyz", token:"xyz" }
//
// Success: { pub:"hex", priv:"hex" }
// Failure: { error:"bad-request|entropy|exists|password|nokey|error", "reason":"str" }
//
// Errors:
//
//	entropy: insufficient entropy available
//	exists: delegate key for this id already exists
//	password: wrong password for master key
//	nokey: master key hasn't been created yet
//	error: system nonsense
func (a *WebAPI) makeDelegate(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args MakeDelegateRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}
		if args.ID == "" {
			sendError(w, http.StatusInternalServerError, "bad-request", "missing 'id'", options)
			return
		}
		if args.Token == "" {
			sendError(w, http.StatusInternalServerError, "bad-request", "missing 'token'", options)
			return
		}

		priv, pub, wif, err := a.keymgr.MakeDelegate(args.ID, args.Token)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
		}

		// response
		res := MakeDelegateResponse{Priv: hex.EncodeToString(priv), Pub: hex.EncodeToString(pub), Wif: wif}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type DelegateKeyRequest struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}
type DelegateKeyResponse struct {
	Priv string `json:"priv"`
	Pub  string `json:"pub"`
}

// API: /get-delegate-key { id:"pup.xyz", token:"hex" }
//
// Success: { priv:"hex", pub:"hex" }
// Failure: { error:"bad-request|not-found|wrong-token|error", "reason":"str" }
//
// Errors:
//
//	not-found: no delegate key found for id
//	wrong-token: wrong token for this key id
func (a *WebAPI) getDelegatePriv(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args DelegateKeyRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}
		if args.ID == "" {
			sendError(w, http.StatusInternalServerError, "bad-request", "missing 'id'", options)
			return
		}
		if args.Token == "" {
			sendError(w, http.StatusInternalServerError, "bad-request", "missing 'token'", options)
			return
		}

		priv, pub, err := a.keymgr.GetDelegatePriv(args.ID, args.Token)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
		}

		// response
		res := DelegateKeyResponse{Priv: hex.EncodeToString(priv), Pub: hex.EncodeToString(pub)}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type DelegatePubRequest struct {
	ID string `json:"id"`
}
type DelegatePubResponse struct {
	Pub string `json:"pub"`
}

// API: /get-delegate-pub { id:"pup.xyz" }
//
// Success: { pub:"hex" }
// Failure: { error:"bad-request|not-found|error", "reason":"str" }
//
// Errors:
//
//	not-found: no delegate key found for id
func (a *WebAPI) getDelegatePub(w http.ResponseWriter, r *http.Request) {
	// request
	options := "GET, POST, OPTIONS"
	id := ""
	if r.Method == http.MethodGet {
		id = r.URL.Query().Get("id")
	} else if r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args DelegatePubRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-request", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}
		id = args.ID
	} else {
		sendOptions(w, r, options)
		return
	}

	if id == "" {
		sendError(w, http.StatusInternalServerError, "bad-request", "missing 'id' query parameter", options)
		return
	}

	key, err := a.keymgr.GetDelegatePub(id)
	if err != nil {
		sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
	}

	// response
	res := DelegatePubResponse{Pub: hex.EncodeToString(key)}
	sendJson(w, res, options)
}

// HELPERS

func sendJson(w http.ResponseWriter, res any, options string) {
	bytes, err := json.Marshal(res)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "error", fmt.Sprintf("encoding JSON: %v", err), options)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Allow", options)
	w.Write(bytes)
}

type WebError struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

func sendError(w http.ResponseWriter, statusCode int, code string, reason string, options string) {
	bytes, err := json.Marshal(WebError{Error: code, Reason: reason})
	if err != nil {
		bytes = []byte(fmt.Sprintf("{\"error\":\"json\",\"reason\":\"encoding JSON: %s\"}", err.Error()))
		statusCode = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Allow", options)
	w.WriteHeader(statusCode)
	w.Write(bytes)
}

func sendOptions(w http.ResponseWriter, r *http.Request, options string) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Allow", options)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", options)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func codeForErr(err error) string {
	if errors.Is(err, bip39.ErrOutOfEntropy) || errors.Is(err, keymgr.ErrOutOfEntropy) {
		return "entropy"
	}
	if errors.Is(err, bip39.ErrWrongWord) {
		return "wordlist"
	}
	if errors.Is(err, bip39.ErrWrongChecksum) {
		return "checksum"
	}
	if errors.Is(err, bip39.ErrWrongLength) {
		return "length"
	}
	if errors.Is(err, keymgr.ErrBadToken) {
		return "token"
	}
	if errors.Is(err, keymgr.ErrWrongPassword) {
		return "password"
	}
	if errors.Is(err, keymgr.ErrWrongToken) {
		return "wrong-token"
	}
	if errors.Is(err, keymgr.ErrWrongMnemonic) {
		return "mnemonic"
	}
	if errors.Is(err, keymgr.ErrKeyExists) || errors.Is(err, internal.ErrAlreadyExists) {
		return "exists"
	}
	if errors.Is(err, internal.ErrNotFound) {
		return "not-found"
	}
	if errors.Is(err, keymgr.ErrNoKey) {
		return "nokey"
	}
	if errors.Is(err, keymgr.ErrTooManyAttempts) {
		return "too-many"
	}
	if errors.Is(err, doge.ErrTooDeep) {
		return "too-deep"
	}
	return "error"
}
