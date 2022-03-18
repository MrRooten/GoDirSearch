package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"github.com/cheggaaa/pb/v3"
	"github.com/pmezard/go-difflib/difflib"
	"go_dir_search/poollib"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
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
func printUsage(helpMessage string) {
	fmt.Println(helpMessage)
}
func printError(errorMessage string,err error) {
	fmt.Println(red("[Error]:")+ errorMessage)
	if err != nil {
		fmt.Println("    " + err.Error())
	}
}

func printInfo(info_message string) {
	fmt.Println(green("[Info]:")+info_message)
}

func printFound(foundMessage string,response *http.Response,size int) {
	statusCode := "["+strconv.Itoa(response.StatusCode)+"] "

	if strconv.Itoa(response.StatusCode)[0] == "4"[0] {
		statusCode = red(statusCode)
	} else if strconv.Itoa(response.StatusCode)[0] == "3"[0] {
		statusCode = green(statusCode)
	} else if strconv.Itoa(response.StatusCode)[0] == "2"[0] {
		statusCode = blue(statusCode)
	}


	res := foundMessage
	res += "\n  " + blue("Status Code: ") + statusCode
	res += "\n  " + blue("Size: ") + green(strconv.Itoa(size)) + red(" bytes")
	if response.Header.Get("Location") != "" {
		res += "\n  " + blue("Redirect to -> ") + response.Header.Get("Location")
	}

	res = "\n" + res
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
	statusCode				string
	is404Verify             bool
	regexString             string
	similarityLevel         int
	notfoundDifferenceRatio float64
	errorPage               string
	curTree                 *DirTree
	curName                 string
	pool                    *poollib.GoroutinePool
	globalContext           *UrlSearchContext
	keyString               string
	treeLock                *sync.Mutex
}

type UrlSearchContext struct {
	handlerLock    *sync.Mutex
	dirQueue       *DirQueue
	dirQueueSave   *DirQueue
	dummyTree      *DirTree
	rootNode       *DirTree
	stringSet      map[string]bool
	sendLock       *sync.Mutex
	lastSendStatus error
	curCountError  int
	countError     int
	timeOut        int
	quit           bool //Set quit flag
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

// GetPathStringList Return the
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
	if reverseDirList == nil {
		return []string{}
	}
	return reverseDirList[1:]
}



func SendRequest(url string, cookie *http.Cookie, header *http.Header, handler HttpHandler, context Context) (string,error) {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	context.globalContext.sendLock.Lock()
	timeout := time.Second * time.Duration(context.globalContext.timeOut)
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: timeout,
	}
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		printError("Can not get a new request",err)
	}
	request.Header.Add("Accept-Encoding", "identity")
	if err != nil {
		printError("Can not get a new request",err)
	}


	if cookie != nil {
		request.AddCookie(cookie)
	}

	if header != nil {
		request.Header = *header
	}
	context.globalContext.sendLock.Unlock()
	response, err := client.Do(request)
	if err != nil {
		printError("Can not send the request to the server", err)
		return "",err
	}
	limitReader := io.LimitReader(response.Body,1024*1024)
	text,err := ioutil.ReadAll(limitReader)
	if err != nil {
		printError("Can not read the IO from the response's body",err)
		return "",err
	}
	context.globalContext.handlerLock.Lock()
	result,err := handler(response, string(text), &context)
	if result == FALSE {
		printError("Error happened:",err)
	}
	context.globalContext.handlerLock.Unlock()
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

	statusCode := context.statusCode
	codeList := strings.Split(statusCode,",")

	for _,code := range codeList {
		notFoundCode,err := strconv.Atoi(code)

		if err != nil {
			printError("Not a valid code",err)
		}
		if response.StatusCode == notFoundCode {
			return TRUE,errors.New("")
		}
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

	if context.keyString != "" {
		if strings.Contains(text,context.keyString) {
			return TRUE,errors.New("Key string not found page")
		}
	}
	if context.similarityLevel > 0 {
		differenceRatio := GetDifferenceRatio(text, context.errorPage)
		if differenceRatio >= context.notfoundDifferenceRatio {
			return TRUE,errors.New("Similarity with error page")
		}
	}
	//If the response status is 3xx and the location path is in context.global_context.string_set then return false
	//Else stores in the strings_set and save to the dir tree
	if strconv.Itoa(response.StatusCode)[0] == "3"[0] {
		location := response.Header.Get("location")
		if context.globalContext.stringSet[location] == true {
			return TRUE,errors.New("Too many redirect to this url")
		} else {
			//This path is different with the default type
			//So this dir add type has it's own way
			context.globalContext.stringSet[location] = true
		}
	}

	//Some page must pass some args to get the page
	//So I use a new way to save to the dir tree

	//End of new way
	curNode := context.curTree
	foundUrl := response.Request.URL
	var addNode *DirTree
	if foundUrl.RawQuery == "" {

		printFound(foundUrl.String(), response, len(text))
		addNode = curNode.AddTreeByName(context.curName)
		*context.globalContext.dirQueueSave = append(*context.globalContext.dirQueueSave, addNode)
	} else {
		path := foundUrl.Path
		//If the url is like http://google.com not like http://google.com/
		if path == "" {
			path = "/"
		}

		filename := path[strings.LastIndex(path,"/")+1:]
		addNode = curNode.AddTreeByName(filename)
		addNode.query = foundUrl.RawQuery
		printFound(foundUrl.String(),response, len(text))
	}
	return TRUE,nil
}

// Verify404Vaild SendRequest's callback function that verify the 404 status code is vaild for not found page or not
func Verify404Vaild(response *http.Response, text string, context *Context) (string,error) {
	if response.StatusCode == 404 {
		context.is404Verify = true
		return TRUE,nil
	}
	context.is404Verify = false
	return TRUE,nil
}

// GetHtml SendRequest's callback function that get the html text as string
func GetHtml(response *http.Response, text string, context *Context) (string,error) {
	return text,nil
}
func isErrorRequest(err error) bool {
	if err == io.EOF {
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
		if i == 0 {
			url = vargs[i].(string)
		} else if i == 1 {
			if vargs[i] == nil {
				cookie = nil
			} else {
				cookie = vargs[i].(*http.Cookie)
			}
		} else if i == 2 {
			if vargs[i] == nil {
				header = nil
			} else {
				header = vargs[i].(*http.Header)
			}
		} else if i == 3 {
			handler = vargs[i].(HttpHandler)
		} else if i == 4 {
			context = vargs[i].(Context)
		}
	}
	_,err := SendRequest(url, cookie,header, handler, context)
	context.globalContext.sendLock.Lock()
	if err != nil {
		context.globalContext.lastSendStatus = err
		if isErrorRequest(err) {
			context.globalContext.curCountError += 1
		}

		if context.globalContext.curCountError > context.globalContext.countError {
			context.globalContext.quit = true
		}
	} else {
		context.globalContext.curCountError = 0
	}
	context.globalContext.sendLock.Unlock()
}

func deepCopy(s string) string {
	var sb strings.Builder
	sb.WriteString(s)
	return sb.String()
}

type Params struct {
	urlString string
	wordlist string
	regexString string
	simLevel int
	depth int
	rate int
	keyString string
	timeOut int
	countError int
	statusCode string
}
func UrlSearch(params *Params) {

	//Inialize Every UrlSearch's Vaule
	urlString := params.urlString
	wordlist := params.wordlist
	regexString := params.regexString
	simLevel := params.simLevel
	depth := params.depth
	rate := params.rate
	keyString := params.keyString
	timeOut := params.timeOut
	countError := params.countError
	statusCode := params.statusCode
	globalContext := UrlSearchContext{}
	globalContext.dirQueue = new(DirQueue)
	globalContext.dirQueueSave = new(DirQueue)
	globalContext.handlerLock = &sync.Mutex{}
	globalContext.rootNode = new(DirTree)
	globalContext.dummyTree = new(DirTree)
	globalContext.stringSet = make(map[string]bool)
	globalContext.lastSendStatus = nil
	globalContext.sendLock = &sync.Mutex{}
	globalContext.countError = countError
	globalContext.timeOut = timeOut
	//Initialize context value
	context := Context{}
	context.notfoundDifferenceRatio = 1.0
	context.globalContext = &globalContext
	context.treeLock = new(sync.Mutex)
	context.statusCode = statusCode
	urlString = strings.Trim(urlString," ")

	//http://test.com/ -> http://test.com
	if strings.HasSuffix(urlString,"/") {
		urlString = urlString[:len(urlString)-1]
	}

	//Verify the 404 signature is valid or not
	context.is404Verify = false
	_,err := SendRequest(urlString+"/"+GenerateRandomString(16), nil, nil, Verify404Vaild, context)
	if err != nil {
		printError("Can not get the not found page",err)
		return 
	}

	context.keyString = keyString
	context.regexString = regexString
	context.similarityLevel = simLevel

	//Get the two non-exist page's difference ratio
	page404_1,err := SendRequest(urlString+"/"+GenerateRandomString(16), nil, nil, GetHtml, context)
	context.errorPage = page404_1
	for i := 0; i < simLevel*3; i++ {
		page404_2,err := SendRequest(urlString+ "/" + GenerateRandomString(16), nil, nil, GetHtml, context,)
		if err != nil {
			printError("Can not get the not found page", err)

		}
		context.notfoundDifferenceRatio = min(GetDifferenceRatio(page404_1, page404_2), context.notfoundDifferenceRatio)
	}

	//Start enumerate the wordlist to get the page is found or not found
	wordlistFile, err := os.Open(wordlist)
	if err != nil {
		printError("Read file error",err)
	}

	scanner := bufio.NewScanner(wordlistFile)
	scanner.Split(bufio.ScanLines)

	var filenameList []string
	for scanner.Scan() {
		filenameList = append(filenameList, scanner.Text())
	}
	wordlistFile.Close()

	//Register a GoroutinePool
	pool := poollib.GoroutinePool{}
	pool.NewGoroutinePool(rate)

	//Build the directory tree
	globalContext.rootNode.name = "/"
	globalContext.rootNode.parent = globalContext.dummyTree
	globalContext.dummyTree.AddTree(globalContext.rootNode)

	//Queue for wide first search in tree.Two queues are for know the depth
	globalContext.dirQueue = new(DirQueue)
	globalContext.dirQueueSave = new(DirQueue)

	//The current search depth
	dp := 0

	*globalContext.dirQueue = append(*globalContext.dirQueue, globalContext.rootNode)
	for len(*globalContext.dirQueue) != 0 && dp != depth {
		curNode := (*globalContext.dirQueue)[0]
		*globalContext.dirQueue = (*globalContext.dirQueue)[1:]
		context.curTree = curNode
		locker := sync.Mutex{}
		bar := pb.StartNew(len(filenameList))

		//last_time := time.Now()
		for iDirName :=0; iDirName <len(filenameList); iDirName++ {
			locker.Lock()
			context.curName = filenameList[iDirName]
			var vargs []interface{}
			var ProcFunction HttpHandler
			ProcFunction = ProcHandler
			requestUrlString := urlString + "/" +strings.Join(append(curNode.GetPathStringList()[:], filenameList[iDirName]),"/")
			newUrlString := deepCopy(requestUrlString)
			locker.Unlock()
			vargs = append(vargs, newUrlString, nil, nil, ProcFunction, context)
			//ptimes :=  (int64(time.Millisecond)* 1000)/( time.Now().UnixNano() - last_time.UnixNano())
			//last_time = time.Now()
			bar.Add(1)
			pool.RunTask(S,vargs)
			//fmt.Printf(blue("\r  (%d/%d) ")+green("%d ")+red("p/s"), i_dir_name,len(filename_list),ptimes)
			if globalContext.quit {
				printInfo("Too many error requests.Stop the requests' sequence")
				//pool.WaitTask()
				return
			}

		}
		bar.Finish()
		pool.WaitTask()
		if len(*globalContext.dirQueue) == 0 {
			dp += 1
			globalContext.dirQueue, globalContext.dirQueueSave = globalContext.dirQueueSave, globalContext.dirQueue
		}
	}
}

func main() {
	urlString := flag.String("u","","Set the url you need to buster")

	urlFile := flag.String("url_file","","Url file")
	wordlist := flag.String("w","","The wordlst")

	simLevel := flag.Int("similarity",1,"Similarity of error page leve(default 1)")
	regexString := flag.String("regex","","Regex String of Error page(default \"\")")
	depth := flag.Int("d",1,"How depth of url need to search(default 1)")
	rate := flag.Int("rate",5,"how fast the search(default 5)")
	keyString := flag.String("key_string","","")
	timeOut := flag.Int("time_out",10,"Request timeout")
	countError := flag.Int("count_error",100,"If too many error,then stop the fuzz")
	statusCode := flag.String("exclude_code","404,429","Exclude status code")
	flag.Parse()
	if *urlString == "" && *urlFile == "" {
		printUsage("Must set the url")
		flag.Usage()
		return
	}

	if *wordlist == "" {
		printUsage("Must set the wordlist")
		return
	}
	params := Params{
		urlString: *urlString,
		wordlist: *wordlist,
		regexString: *regexString,
		simLevel: *simLevel,
		depth: *depth,
		rate: *rate,
		keyString: *keyString,
		timeOut: *timeOut,
		countError: *countError,
		statusCode: *statusCode,
	}
	if *urlString != "" {
		UrlSearch(&params)
	}

	//UrlSearch("http://zhihu.com",".\\test_wordlist.txt","",1,1,1)
}
