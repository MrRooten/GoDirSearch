package main

import (
	"fmt"
	"github.com/pmezard/go-difflib/difflib"
	"io"
	"net/http"
)
func MyNewRequest(method string,url string,reader io.Reader) (*http.Request,error){
	req,err := http.NewRequest(http.MethodGet,"",nil)
	return req,err
}

func GetDifferenceRatio(s1 string, s2 string) float64 {

	seqm := difflib.NewMatcher(s3, s4)
	return seqm.Ratio()
}

func main() {
	fmt.Println(GetDifferenceRatio("abcd","abcg"))
}
