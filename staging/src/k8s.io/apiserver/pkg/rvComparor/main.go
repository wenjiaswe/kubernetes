package main

import (
	//"flag"
	"fmt"
	"os"
	"bufio"
	"strings"
	"sort"
	"log"
	"flag"
)

const (
	message = `List 0 are the resourceVersions of events got from etcd (ETCD)
List 1 are the resourceVersions of events after processing event from etcd (OUTETCD)
List 2 are the resourceVersions of events going into API server (INAPISERVER)
List 3 are the resourceVersions of events sent out from API server (OUTAPISERVER)
List 4 are the resourceVersions of events got into scheduler (SCHEDULER)
List 5 are the resourceVersions of events got into controller-manager (CONTROLLERMANAGER)
Please find details here: https://github.com/kubernetes/kubernetes/pull/61067
`
)
var rvLists [][]string

func main() {
	fmt.Println(message)
	logDir := flag.String("log-dir", "./testlogs", "directory of the log files")
	eventKey := flag.String("event-key", "", "key of the interested event")
	baseList := flag.String("base-list", "", "base list that you want to compare with (choose one from above)")
	flag.Parse()


	//base := flag.Args()[0]
	var baseNum int
	switch strings.ToUpper(*baseList){
	case "ETCD":
		baseNum = 0
	case "OUTETCD":
		baseNum = 1
	case "INAPISERVER":
		baseNum = 2
	case "OUTAPISERVER":
		baseNum = 3
	case "SCHEDULER":
		baseNum = 4
	case "CONTROLLERMANAGER":
		baseNum = 5
	default:
		baseNum = 0
	}

	rvLists = make([][]string, 6)

	readLogsAndFillRVLists4APIServer(*logDir, *eventKey)
	readLogsAndFillRVLists4Client(*logDir, *eventKey)

	for _, rvList := range rvLists{
		sort.Strings(rvList)
		fmt.Println(rvList)
	}
	compareLists(baseNum)
}

func readLogsAndFillRVLists4APIServer(logDir string, eventKey string){
	logEntries, err := readLines(logDir + "/kube-apiserver.log")
	if err != nil {
		log.Fatalf("Failed reading lines: %v", err)
	}
	fillRVList4APIServer(logEntries, rvLists, eventKey)
}

func readLogsAndFillRVLists4Client(logDir string, eventKey string){
	sLogEntries, err := readLines(logDir + "/kube-scheduler.log")
	if err != nil {
		log.Fatalf("Failed reading lines: %v", err)
	}
	fillRVList4Client(sLogEntries, rvLists, 4, eventKey)

	cmLogEntries, err := readLines(logDir + "/kube-controller-manager.log")
	if err != nil {
		log.Fatalf("Failed reading lines: %v", err)
	}
	fillRVList4Client(cmLogEntries, rvLists, 5, eventKey)
}
// readLines reads a whole file into memory
// and returns a slice of its lines.
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// writeLines writes the lines to the given file.
func fillRVList4APIServer(lines []string, rvLists [][]string, eventKey string) {
	for _, line := range lines {
		result := strings.Split(line, ",")
		eyeCatcher := result[0]
		if ( eyeCatcher == "SWAT" ) && ( eventKey == "" || result[3] == eventKey){
			rvStr := result[4]
			switch result[1] {
			case "etcd3/watcher/transform/curObj", "etcd3/watcher/transform/oldObj":
				if sort.SearchStrings(rvLists[0], rvStr) == len(rvLists[0]) {
					rvLists[0] = append(rvLists[0], rvStr)
				}
			case "etcd3/watcher/processEvent":
				rvLists[1] = append(rvLists[1], rvStr)
			case "watch_cache/processEvent":
				rvLists[2] = append(rvLists[2], rvStr)
			case "cacher/sendWatchCacheEvent":
				if sort.SearchStrings(rvLists[3], rvStr) == len(rvLists[3]) {
					rvLists[3] = append(rvLists[3], rvStr)
				}
			case "reflector/watchHandler":
			//	if sort.SearchStrings(rvLists[4], rvStr) == len(rvLists[4]) {
			//		rvLists[4] = append(rvLists[4], rvStr)
			//	}
			}
		}
	}
	return
}

// writeLines writes the lines to the given file.
func fillRVList4Client(lines []string, rvLists [][]string, listNum int, eventKey string) {
	for _, line := range lines {
		result := strings.Split(line, ",")
		eyeCatcher := result[0]
		if ( eyeCatcher == "SWAT" ) && ( eventKey == "" || result[3] == eventKey){
			rvStr := result[4]
			switch result[1] {
			case "reflector/watchHandler":
				if sort.SearchStrings(rvLists[listNum], rvStr) == len(rvLists[listNum]) {
					rvLists[listNum] = append(rvLists[listNum], rvStr)
				}
				break
			default:
				fmt.Println(line)
			}
		}
	}
	return
}

func compareLists(baseNum int){
	baseList := rvLists[baseNum]
	for i := 0; i < 6; i++ {
		if i == baseNum {
			continue
		}
		fmt.Printf("\nComparing base list %d and current list %d\n", baseNum, i)
		isSame := compareWithBase(baseList, rvLists[i])
		switch isSame {
		case false:
			fmt.Printf("OUCH!!! List %d is DIFFERENT from base list\n", i)
		case true:
			fmt.Printf("Fine~ List %d is same as base list\n", i)
		}
	}
	fmt.Println("\nDone comparing")
}

func compareWithBase(baseRvList []string, currRvList []string) bool{
	bl := len(baseRvList)
	cl := len(currRvList)
	if bl != cl {
		fmt.Printf("base rv list has %d events but current rv list has %d events\n", bl, cl)
		return false
	}

	for i := 0; i < len(baseRvList); i++ {
		if baseRvList[i] != currRvList[i]{
			fmt.Printf("Check event with resourceVersion %d in base rv list and event with resourceVersion %d in current rv list\n", bl, cl)
			return false
		}
	}
	return true
}