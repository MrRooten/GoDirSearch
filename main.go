package main

import (
	"bufio"
	"flag"
	"fmt"
	"go_dir_search/poollib"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
)

const TRUE = "true"
const FALSE = "false"

var wg *sync.WaitGroup

func print_usage(help_message string) {

}
func print_error(error_message string,err error) {

}

func print_info(info_message string) {

}

func print_found(found_message string,response *http.Response) {
	res := "["+strconv.Itoa(response.StatusCode)+"] " + found_message
	fmt.Println(res)
}
func min(n1 float64, n2 float64) float64 {
	if n1 > n2 {
		return n2
	}

	return n1
}

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

func StringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func GenerateRandomString(length int) string {
	return StringWithCharset(length, charset)
}

func splitChars(s string) []string {
	chars := make([]string, 0, len(s))
	// Assume ASCII inputs
	for i := 0; i != len(s); i++ {
		chars = append(chars, string(s[i]))
	}
	return chars
}

func GetDifferenceRatio(s1 string, s2 string) float64 {
	seqm := difflib.NewMatcher(splitChars(s1), splitChars(s2))
	return seqm.Ratio()
}

type Context struct {
	is_404_verify             bool
	regexString               string
	similarity_level          int
	notfound_difference_ratio float64
	error_page                string
	cur_tree                  *DirTree
	cur_name                  string
	pool               		  *poollib.GoroutinePool
}

type DirTree struct {
	name     string
	subtrees []DirTree
	parent   *DirTree
}

func (dir *DirTree) AddTree(subdir DirTree) {
	dir.subtrees = append(dir.subtrees, subdir)
	dir.subtrees[len(dir.subtrees)-1].parent = dir
}

func (dir *DirTree) AddTreeByName(name string) *DirTree {
	tree := DirTree{}
	tree.name = name
	tree.parent = dir
	dir.subtrees = append(dir.subtrees, tree)
	return &tree
}
//Return the
func (dir *DirTree) GetPathStringList() []string {
	var curNode *DirTree
	var dirList []string
	for curNode = dir; curNode.parent != nil; curNode = curNode.parent {
		dirList = append(dirList, curNode.name)
	}

	var reverseDirList []string
	for i := len(dirList) - 1; i >= 0; i-- {
		reverseDirList = append(reverseDirList, dirList[i])
	}
	return reverseDirList[1:]
}

type DirQueue []DirTree

var handlerLock sync.Mutex

type HttpHandler func(response *http.Response, text string, context *Context) string

func SendRequest(url string, cookie *http.Cookie, header *http.Header, handler HttpHandler, context *Context) string {
	//defer wg.Done()
	client := &http.Client{}
	request, err := http.NewRequest("GET", url, nil)

	if err != nil {
		print_error("Can not get a new request",err)
		return "error"
	}

	if cookie != nil {
		request.AddCookie(cookie)
	}

	if header != nil {
		request.Header = *header
	}

	response, err := client.Do(request)
	if err != nil {
		print_error("Can not send the request to the server",err)
		return "error"
	}

	text, err := ioutil.ReadAll(response.Body)
	if err != nil {
		print_error("Can not read the IO from the response's body",err)
		return "error"
	}
	//handlerLock.Lock()
	result := handler(response, string(text), context)
	//handlerLock.Unlock()
	return result
}

var dir_queue DirQueue
var dir_queue_save DirQueue

func ProcHandler(response *http.Response, text string, context *Context) string {
	//If status code is 429,then decrease the scan rate
	if response.StatusCode == 429 {
		//Jar all goroutines wait in pool
	}

	if context.is_404_verify {
		if response.StatusCode == 404 {
			return FALSE
		}
	}

	if context.regexString != "" {
		matched, err := regexp.Match(context.regexString, []byte(text))
		if err != nil {

		}

		if matched {
			return FALSE
		}
	}

	if context.similarity_level > 0 {
		difference_ratio := GetDifferenceRatio(text, context.error_page)
		if difference_ratio >= context.notfound_difference_ratio {
			return FALSE
		}
	}

	cur_node := context.cur_tree
	found_url := response.Request.URL
	print_found(found_url.String(),response)
	add_node := cur_node.AddTreeByName(context.cur_name)
	dir_queue_save = append(dir_queue_save, *add_node)
	return TRUE
}

//SendRequest's callback function that verify the 404 status code is vaild for not found page or not
func Verify404Vaild(response *http.Response, text string, context *Context) string {
	if response.StatusCode == 404 {
		return TRUE
	}

	return FALSE
}

//SendRequest's callback function that get the html text as string
func GetHtml(response *http.Response, text string, context *Context) string {
	return text
}


func S(vargs []interface{}) {
	var url string
	var cookie *http.Cookie
	var header *http.Header
	var handler HttpHandler
	var context *Context

	for i,_:=range vargs {
		if (i == 0) {
			url = vargs[i].(string)
		} else if (i == 1) {
			if vargs[i] == nil {
				cookie = nil
			} else {
				cookie = vargs[i].(*http.Cookie)
			}
		} else if (i == 2) {
			if vargs[i] == nil {
				header = nil
			} else {
				header = vargs[i].(*http.Header)
			}
		} else if (i == 3) {
			handler = vargs[i].(HttpHandler)
		} else if (i == 4) {
			context = vargs[i].(*Context)
		}
	}
	SendRequest(url, cookie,header, handler, context)
}

var dummy_tree = DirTree{}
var root_node = DirTree{}

//https://baidu.com/ -> https://baidu.com

func MainProcess(url_string string, wordlist string, regexString string, sim_level int, depth int, rate int) {

	context := Context{}
	//Initialize value
	context.notfound_difference_ratio = 1.0
	url_string = strings.Trim(url_string," ")
	if strings.HasSuffix(url_string,"/") {
		url_string = url_string[:len(url_string)-1]
	}

	//Verify the 404 signature is valid or not
	context.is_404_verify = false
	if SendRequest(url_string+GenerateRandomString(16), nil, nil, Verify404Vaild, nil) == "true" {
		context.is_404_verify = true
	}

	context.regexString = regexString
	context.similarity_level = sim_level

	//Get the two non-exist page's difference ratio
	page404_1 := SendRequest(url_string+"/"+GenerateRandomString(16), nil, nil, GetHtml, nil)
	context.error_page = page404_1
	for i := 0; i < sim_level*5; i++ {
		page404_2 := SendRequest(url_string+ "/" + GenerateRandomString(16), nil, nil, GetHtml, nil)
		context.notfound_difference_ratio = min(GetDifferenceRatio(page404_1, page404_2), context.notfound_difference_ratio)
	}

	//Start enumerate the wordlist to get the page is found or not found
	wordlist_file, err := os.Open(wordlist)
	if err != nil {
		print_error("Read file error",err)
	}

	scanner := bufio.NewScanner(wordlist_file)
	scanner.Split(bufio.ScanLines)

	var filename_list []string
	for scanner.Scan() {
		filename_list = append(filename_list, scanner.Text())
	}
	wordlist_file.Close()

	//Register a GoroutinePool
	pool := poollib.GoroutinePool{}
	pool.NewGoroutinePool(rate)

	//Build the directory tree
	root_node.name = "/"
	root_node.parent = &dummy_tree
	dummy_tree.AddTree(root_node)

	dir_queue = DirQueue{}

	dir_queue_save = DirQueue{}

	dp := 0

	for i := 0; i < len(dummy_tree.subtrees); i++ {
		dir_queue = append(dir_queue, dummy_tree.subtrees[i])
	}

	for len(dir_queue) != 0 && dp != depth {
		cur_node := dir_queue[0]
		dir_queue = dir_queue[1:]

		for i_dir_name := range filename_list {
			context.cur_tree = &cur_node
			context.cur_name = filename_list[i_dir_name]
			var vargs []interface{}
			var ProcFunction HttpHandler
			ProcFunction = ProcHandler
			request_url_string := url_string + "/" +strings.Join(append(cur_node.GetPathStringList()[:],filename_list[i_dir_name]),"/")
			vargs = append(vargs, request_url_string, nil, nil, ProcFunction, &context)
			pool.RunTask(S,vargs)
		}
		pool.WaitTask()
		if len(dir_queue) == 0 {
			dp += 1
			dir_queue, dir_queue_save = dir_queue_save, dir_queue
		}
	}
}

func main() {
	url_string := flag.String("u","","Set the url you need to buster")
	if *url_string == "" {
		print_usage("Must set the url")
		return
	}
	wordlist := flag.String("w","","The wordlst")
	if *wordlist == "" {
		print_usage("Must set the wordlist")
		return
	}
	sim_level := flag.Int("s",1,"Similarity of error page leve(default 1)")
	regex_string := flag.String("r","","Regex String of Error page(default \"\")")
	depth := flag.Int("d",1,"How depth of url need to search(default 1)")
	rate := flag.Int("rate",5,"how fast the search(default 5)")
	MainProcess(*url_string,*wordlist,*regex_string,*sim_level,*depth,*rate)
}
