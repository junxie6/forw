package main

import (
	"bytes"
	"fmt"
	"encoding/json"
	"io"
	"io/ioutil"
	"flag"
	"net"
	"net/http"
	"net/http/httputil"
)

var (
	file = flag.String("f", "config.json", "Path to config file")
	debug = flag.Bool("d", false, "Debug messages")
)

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

func DuplicateRequest(request *http.Request) (request1 *http.Request, request2 *http.Request) {
	b1 := new(bytes.Buffer)
	b2 := new(bytes.Buffer)
	w := io.MultiWriter(b1, b2)
	io.Copy(w, request.Body)
	defer request.Body.Close()
	request1 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b1},
		Host:          request.Host,
		ContentLength: request.ContentLength,
		Close:         true,
	}
	request2 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b2},
		Host:          request.Host,
		ContentLength: request.ContentLength,
		Close:         true,
	}
	return
}

func (h httpHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered", r)
		}
	}()
	mainReq, otherReq := DuplicateRequest(req) // dup the whole request
	b, _ := ioutil.ReadAll(otherReq.Body) // store the body once since it won't change
	for _, elem := range conf.Forwards {
		go func(el Target){
			_, otherReq = DuplicateRequest(req)
			otherReq.Body = ioutil.NopCloser(bytes.NewBuffer(b)) // reset the body to original 
			MakeHTTPReq(string(el), otherReq, b) // ignore response
		}(elem)
	}
	func(){
		resp := MakeHTTPReq(string(conf.Proxy), mainReq, b) // forward the original req and get resp
		body, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close() // close resp after reading body!!!
		if err != nil {
			fmt.Printf("Failed read resp from %s, %v\n", conf.Proxy, err)
			return
		}
		w.Write(body) // write the response back
	}()
}

func MakeHTTPReq(t string, req *http.Request, b []byte) (resp *http.Response){
	req.Body = ioutil.NopCloser(bytes.NewBuffer(b)) // reset the body to original 
	tcpConn, err := net.Dial("tcp", t)
	if err != nil {
		fmt.Printf("Errorn in tcp req to %s: %v\n", t, err)
		return
	}
	httpConn := httputil.NewClientConn(tcpConn, nil)
	defer httpConn.Close() 
	err = httpConn.Write(req)
	if err != nil {
		fmt.Printf("Failed to write http data to %s, %v\n", t, err)
		return
	}
	resp, err = httpConn.Read(req)
	if err != nil && err != httputil.ErrPersistEOF {
		fmt.Printf("Failed to read from %s: %v\n", t, err)
		return
	}
	return resp
}

type httpHandler struct{}

// static definition of our config json
type Config struct{
	Listen Target		// must match top level keys in json file
	Proxy Target		// must match top level keys in json file
	Forwards []Target	// must match top level keys in json file
}

type Target string

var conf Config // for global access

func main(){
	flag.Parse()
	fmt.Println("Listening for requests on: %s", conf.Proxy)
	content, err := ioutil.ReadFile(*file)
	if err != nil {
		fmt.Print("Error reading config file: ", err)
	}
	err = json.Unmarshal(content, &conf) // parse json into conf
	if err != nil {
		fmt.Print("Error in Json: ", err)
	}
	fmt.Printf("%#v\n", conf)
	tcpListen, err := net.Listen("tcp", string(conf.Listen))
	fmt.Printf("Listening on port: %s\n", conf.Listen)
	http.Serve(tcpListen, httpHandler{})
}