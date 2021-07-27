package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"github.com/cheggaaa/pb/v3"
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
type DirQueue []*DirTree
type HttpHandler func(response *http.Response, text string, context *Context) (string,error)

func red(str string) string {
	return "\033[1;31m"+str+"\033[39m"
}

func blue(str string) string {
	return "\033[1;34m"+str+"\033[39m"
}

func green(str string) string {
	return "\033[1;32m"+str+"\033[39m"
}
func print_usage(help_message string) {
	fmt.Println(help_message)
}
func print_error(error_message string,err error) {
	fmt.Println(red("[Error]:")+error_message)
	if err != nil {
		fmt.Println("    " + err.Error())
	}
}

func print_info(info_message string) {
	fmt.Println(green("[Info]:")+info_message)
}

func print_found(found_message string,response *http.Response,size int) {
	status_code := "["+strconv.Itoa(response.StatusCode)+"] "

	if strconv.Itoa(response.StatusCode)[0] == "4"[0] {
		status_code = red(status_code)
	} else if (strconv.Itoa(response.StatusCode)[0] == "3"[0]) {
		status_code = green(status_code)
	} else if (strconv.Itoa(response.StatusCode)[0] == "2"[0]) {
		status_code = blue(status_code)
	}


	res :=  found_message
	res += "\n  " + blue("Status Code: ") + status_code
	res += "\n  " + blue("Size: ") + green(strconv.Itoa(size)) + red(" bytes")
	if response.Header.Get("Location") != "" {
		res += "\n  " + blue("Redirect to -> ") + response.Header.Get("Location")
	}
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
	global_context			  *UrlSearchContext
	key_string                string
	tree_lock				  *sync.Mutex
}

type UrlSearchContext struct {
	handlerLock 		      *sync.Mutex
	dir_queue 				  *DirQueue
	dir_queue_save 			  *DirQueue
	dummy_tree 				  *DirTree
	root_node 				  *DirTree
	string_set 				  map[string]bool
	send_lock				  *sync.Mutex
	last_send_status		  error
	cur_count_error			  int
	count_error			      int
	time_out                  int
	quit  				      bool //Set quit flag
}
type DirTree struct {
	name     string
	subtrees []*DirTree
	parent   *DirTree
	query    string
}

func (dir *DirTree) AddTree(subdir *DirTree) {
	dir.subtrees = append(dir.subtrees, subdir)
	dir.subtrees[len(dir.subtrees)-1].parent = dir
}

func (dir *DirTree) AddTreeByName(name string) *DirTree {
	tree := DirTree{}
	tree.name = name
	tree.parent = dir
	dir.subtrees = append(dir.subtrees, &tree)
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



func SendRequest(url string, cookie *http.Cookie, header *http.Header, handler HttpHandler, context Context) (string,error) {

	context.global_context.send_lock.Lock()
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		print_error("Can not get a new request",err)
		return "",err
	}


	if cookie != nil {
		request.AddCookie(cookie)
	}

	if header != nil {
		request.Header = *header
	}
	context.global_context.send_lock.Unlock()
	response, err := client.Do(request)
	if err != nil {
		print_error("Can not send the request to the server",err)
		if is_error_request(err) {
			context.global_context.cur_count_error += 1
		}

		if context.global_context.cur_count_error > context.global_context.count_error {
			context.global_context.quit = true
			return FALSE,err
		}
		return "",err
	}

	text, err := ioutil.ReadAll(response.Body)
	if err != nil {
		print_error("Can not read the IO from the response's body",err)
		return "",err
	}
	context.global_context.handlerLock.Lock()
	result,err := handler(response, string(text), &context)
	if result == FALSE {
		print_error("Error happened:",err)
	}
	context.global_context.handlerLock.Unlock()
	return result,nil
}

func ProcHandler(response *http.Response, text string, context *Context) (string,error) {
	//If status code is 429,then decrease the scan rate
	if response.StatusCode == 429 {
		//Jar all goroutines wait in pool
		return TRUE,errors.New("Too many request")
	}


	if response.StatusCode == 404 {
		return TRUE,errors.New("Not found page")
	}


	if context.regexString != "" {
		matched, err := regexp.Match(context.regexString, []byte(text))
		if err != nil {
			return FALSE,errors.New("Regex string have error:" + err.Error())
		}

		if matched {
			return FALSE, errors.New("Regex not found page")
		}
	}

	if context.key_string != "" {
		if strings.Contains(text,context.key_string) {
			return TRUE,errors.New("Key string not found page")
		}
	}
	if context.similarity_level > 0 {
		difference_ratio := GetDifferenceRatio(text, context.error_page)
		if difference_ratio >= context.notfound_difference_ratio {
			return TRUE,errors.New("Similarity with error page")
		}
	}
	//If the response status is 3xx and the location path is in context.global_context.string_set then return false
	//Else stores in the strings_set and save to the dir tree
	if strconv.Itoa(response.StatusCode)[0] == "3"[0] {
		location := response.Header.Get("location")
		if context.global_context.string_set[location] == true {
			return TRUE,errors.New("Too many redirect to this url")
		} else {
			//This path is different with the default type
			//So this dir add type has it's own way
			context.global_context.string_set[location] = true
		}
	}

	//Some page must pass some args to get the page
	//So I use a new way to save to the dir tree

	//End of new way
	cur_node := context.cur_tree
	found_url := response.Request.URL
	var add_node *DirTree
	if found_url.RawQuery == "" {

		print_found(found_url.String(), response, len(text))
		add_node = cur_node.AddTreeByName(context.cur_name)
		*context.global_context.dir_queue_save = append(*context.global_context.dir_queue_save, add_node)
	} else {
		path := found_url.Path
		//If the url is like http://google.com not like http://google.com/
		if path == "" {
			path = "/"
		}

		filename := path[strings.LastIndex(path,"/")+1:]
		add_node = cur_node.AddTreeByName(filename)
		add_node.query = found_url.RawQuery
		print_found(found_url.String(),response, len(text))
	}
	return TRUE,nil
}

//SendRequest's callback function that verify the 404 status code is vaild for not found page or not
func Verify404Vaild(response *http.Response, text string, context *Context) (string,error) {
	if response.StatusCode == 404 {
		context.is_404_verify = true
		return TRUE,nil
	}
	context.is_404_verify = false
	return TRUE,nil
}

//SendRequest's callback function that get the html text as string
func GetHtml(response *http.Response, text string, context *Context) (string,error) {
	return text,nil
}
func is_error_request(err error) bool {
	if strings.Contains(err.Error(),"EOF") {
		return true
	}
	return false
}

func S(vargs []interface{}) {
	var url string
	var cookie *http.Cookie
	var header *http.Header
	var handler HttpHandler
	var context Context

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
			context = vargs[i].(Context)
		}
	}
	_,err := SendRequest(url, cookie,header, handler, context)

	if err != nil {
		context.global_context.send_lock.Lock()
		context.global_context.last_send_status = err
		context.global_context.send_lock.Unlock()
	}
}

func deepCopy(s string) string {
	var sb strings.Builder
	sb.WriteString(s)
	return sb.String()
}

func UrlSearch(url_string string, wordlist string, regexString string, sim_level int, depth int, rate int,key_string string,time_out int,count_error int) {

	//Inialize Every UrlSearch's Vaule
	global_context := UrlSearchContext{}
	global_context.dir_queue = new(DirQueue)
	global_context.dir_queue_save = new(DirQueue)
	global_context.handlerLock = &sync.Mutex{}
	global_context.root_node = new(DirTree)
	global_context.dummy_tree = new(DirTree)
	global_context.string_set = make(map[string]bool)
	global_context.last_send_status = nil
	global_context.send_lock = &sync.Mutex{}
	global_context.count_error = count_error
	//Initialize context value
	context := Context{}
	context.notfound_difference_ratio = 1.0
	context.global_context = &global_context
	context.tree_lock = new(sync.Mutex)
	url_string = strings.Trim(url_string," ")
	if strings.HasSuffix(url_string,"/") {
		url_string = url_string[:len(url_string)-1]
	}

	//Verify the 404 signature is valid or not
	context.is_404_verify = false
	_,err := SendRequest(url_string+"/"+GenerateRandomString(16), nil, nil, Verify404Vaild, context)
	if err != nil {
		print_error("Can not get the not found page",err)
		return 
	}

	context.key_string = key_string
	context.regexString = regexString
	context.similarity_level = sim_level

	//Get the two non-exist page's difference ratio
	page404_1,err := SendRequest(url_string+"/"+GenerateRandomString(16), nil, nil, GetHtml, context)
	context.error_page = page404_1
	for i := 0; i < sim_level*3; i++ {
		page404_2,err := SendRequest(url_string+ "/" + GenerateRandomString(16), nil, nil, GetHtml, context,)
		if err != nil {
			print_error("Can not get the not found page",err)
		}
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
	global_context.root_node.name = "/"
	global_context.root_node.parent = global_context.dummy_tree
	global_context.dummy_tree.AddTree(global_context.root_node)

	global_context.dir_queue = new(DirQueue)

	global_context.dir_queue_save = new(DirQueue)

	dp := 0

	*global_context.dir_queue = append(*global_context.dir_queue,global_context.root_node)
	for len(*global_context.dir_queue) != 0 && dp != depth {
		cur_node := (*global_context.dir_queue)[0]
		*global_context.dir_queue = (*global_context.dir_queue)[1:]
		context.cur_tree = cur_node
		locker := sync.Mutex{}
		bar := pb.StartNew(len(filename_list))
		for i_dir_name:=0;i_dir_name<len(filename_list);i_dir_name++ {
			locker.Lock()
			context.cur_name = filename_list[i_dir_name]
			var vargs []interface{}
			var ProcFunction HttpHandler
			ProcFunction = ProcHandler
			request_url_string := url_string + "/" +strings.Join(append(cur_node.GetPathStringList()[:],filename_list[i_dir_name]),"/")
			new_url_string := deepCopy(request_url_string)
			locker.Unlock()
			vargs = append(vargs, new_url_string, nil, nil, ProcFunction, context)
			bar.Increment()
			pool.RunTask(S,vargs)

			if (global_context.quit) {
				print_info("Too many error requests.Stop the requests' sequence")
				//pool.WaitTask()
				return
			}
		}
		bar.Finish()
		pool.WaitTask()
		if len(*global_context.dir_queue) == 0 {
			dp += 1
			global_context.dir_queue, global_context.dir_queue_save = global_context.dir_queue_save, global_context.dir_queue
		}
	}
}

func main() {
	url_string := flag.String("u","","Set the url you need to buster")

	url_file := flag.String("url_file","","Url file")
	wordlist := flag.String("w","","The wordlst")

	sim_level := flag.Int("s",1,"Similarity of error page leve(default 1)")
	regex_string := flag.String("r","","Regex String of Error page(default \"\")")
	depth := flag.Int("d",1,"How depth of url need to search(default 1)")
	rate := flag.Int("rate",5,"how fast the search(default 5)")
	key_string := flag.String("key_string","","")
	time_out := flag.Int("time_out",10,"Request timeout")
	count_error := flag.Int("count_error",100,"If too many error,then stop the fuzz")
	flag.Parse()
	if *url_string == "" && *url_file == "" {
		print_usage("Must set the url")
		return
	}

	if *wordlist == "" {
		print_usage("Must set the wordlist")
		return
	}
	if *url_string != "" {
		UrlSearch(*url_string, *wordlist, *regex_string, *sim_level, *depth, *rate,*key_string,*time_out,*count_error)
	} else if *url_file !=""{
		urllist_file, err := os.Open(*url_file)
		if err != nil {
			print_error("Read file error",err)
		}

		scanner := bufio.NewScanner(urllist_file)
		scanner.Split(bufio.ScanLines)

		var url_list []string
		for scanner.Scan() {
			url_list = append(url_list, scanner.Text())
		}
		urllist_file.Close()

		for i:=0;i < len(url_list);i++ {
			UrlSearch(url_list[i],*wordlist,*regex_string,*sim_level,*depth,*rate,*key_string,*time_out,*count_error)
		}
	}

	//UrlSearch("http://zhihu.com",".\\test_wordlist.txt","",1,1,1)
}
