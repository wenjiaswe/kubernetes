package main

import (
	"github.com/spf13/cobra"
	"fmt"
	"os"
	"bufio"
	"strings"
	"log"
	"strconv"
	"io/ioutil"
	"path/filepath"
	//"time"
	"sort"
)

const (
	message = `List 0: "etcd3/watcher/transform/curObj", "etcd3/watcher/transform/oldObj"
List 1: "etcd3/watcher/processEvent"
List 2: "watch_cache/processEvent
List 3: "cacher/dispatchEvent"
List 4: "cacher/add0"
List 5: "cacher/add1", "cacher/add2", "cacher/add/case3"
List 6: "cacher/send0"
List 7: "cacher/send1"
List 8: "cacher/send2"
List 9: scheduler reflecter
List 10: kcm reflecter
Please find details here: https://github.com/kubernetes/kubernetes/pull/61067
`
)


var (
	eventCheckCmd = &cobra.Command{
		Short: "A tool to check if there is any missing event based on resourceVersion.",
		Long: message,
		Run: func(cmd *cobra.Command, args []string) {
			runEventCheck()
		},
	}
	ecOpts = eventCheckOpts{}
	rvLists [][]int
	eventLists [][]eventTrackerEntry
	apiEventList []eventTrackerEntry
	schedulerEventList []eventTrackerEntry
	kcmEventList []eventTrackerEntry
	apiLogEntries []string
)

type eventCheckOpts struct {
	logDir string
	baseList string
	podName string
	podNameHas string
	listtype string
	eventdiff bool
}

func (e eventTrackerEntry) Print(){
	fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.dataPoint, e.timestamp, e.eventType, e.namespace, e.objName, e.reflectType, e.resourceVersion)
	return
}

type eventTrackerEntry struct {
	dataPoint       string
	timestamp       string
	eventType       string
	namespace       string
	objName         string
	reflectType     string
	resourceVersion string
}

func main() {

	flags := eventCheckCmd.Flags()
	flags.StringVar(&ecOpts.logDir, "logDir", "/var/log", "absolute path to the log directory")
	flags.StringVar(&ecOpts.baseList, "baseList", "ETCD", "base list that you want to compare with (choose one from etcd, outetcd, inapiserver, outapiserver, scheduler, controllermanager)")
	flags.StringVar(&ecOpts.podName, "podName", "", "interested pod name")
	flags.StringVar(&ecOpts.podNameHas, "podNameHas", "", "interested in pods whose name contains this string")
	flags.StringVar(&ecOpts.listtype, "listtype", "", "listing event or rv")
	flags.BoolVar(&ecOpts.eventdiff, "eventdiff", false, "listing diff event for each pod")
	eventCheckCmd.Execute()

}


func runEventCheck(){
	fmt.Println("here")
	baseNum := 0
	//var baseNum int
	//switch strings.ToUpper(ecOpts.baseList){
	//case "ETCD":
	//	baseNum = 0
	//case "OUTETCD":
	//	baseNum = 1
	//case "INAPISERVER":
	//	baseNum = 2
	//case "OUTAPISERVER":
	//	baseNum = 3
	////case "CACHERADD":
	////	baseNum = 4
	//case "SCHEDULER":
	//	baseNum = 4
	//case "CONTROLLERMANAGER":
	//	baseNum = 5
	//default:
	//	baseNum = 0
	//}


	//for {
	//	time.Sleep(time.Duration(3) * time.Second)
		fmt.Println("Checking logs...")
		err := FilterDirs("kube-apiserver.log", ecOpts.logDir)

		//fmt.Println(kaslogs)
		if err != nil {
			log.Fatal("Error reading apiserver log from logDir!")
		}
		//for _, kaslog := range kaslogs {
		//	currEventList, currLogEntries, err := readLogs(kaslog)
		//	if err != nil {
		//		fmt.Println(err)
		//		os.Exit(1)
		//	}
		//	apiEventList = append(apiEventList, currEventList...)
		//	apiLogEntries = append(apiLogEntries, currLogEntries...)
		//}

		err = FilterDirs("kube-scheduler.log", ecOpts.logDir)
		if err != nil {
			log.Fatal("Error reading scheduler log from logDir!")
		}
		//for _, slog := range slogs {
		//	currEventList, _, err := readLogs(slog)
		//	if err != nil {
		//		fmt.Println(err)
		//		os.Exit(1)
		//	}
		//	schedulerEventList = append(schedulerEventList, currEventList...)
		//}

		err = FilterDirs("kube-controller-manager.log", ecOpts.logDir)
		if err != nil {
			log.Fatal("Error reading scheduler log from logDir!")
		}
		//for _, kcmlog := range kcmlogs {
		//	currEventList, _, err := readLogs(kcmlog)
		//	if err != nil {
		//		fmt.Println(err)
		//		os.Exit(1)
		//	}
		//	kcmEventList = append(kcmEventList, currEventList...)
		//}
	//}

	fmt.Printf("schedulerlist: %d, kcmlist: %d\n", len(schedulerEventList), len(kcmEventList))


	podsList := getPodsList(apiLogEntries)
	podCnt := len(podsList)
	fmt.Printf("\n%d pods found\n\n", podCnt)
	falsePodsCnt := 0

	fmt.Printf("\nChecking resourceVersion of event for each pod...\n")
	var falsePodList []string

	for _, podName := range podsList {
		rvLists = make([][]int, 11)
		eventLists = make([][]eventTrackerEntry, 11)
		fillList4APIServer(apiEventList,  podName)
		fillList4Client(schedulerEventList, 9, podName)
		fillList4Client(kcmEventList, 10, podName)
		for _, rvlist := range rvLists {
			sort.Ints(rvlist)
		}
		falseList, isSame := compareLists(baseNum, podName)
		if !isSame {
			fmt.Printf("Pod %s is not right in lists: ", podName)
			fmt.Println(falseList)
			fmt.Println("++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++")
			falsePodsCnt ++
			falsePodList = append(falsePodList, podName)
		}
	}

	if falsePodsCnt != 0 {
		fmt.Println("==================================Here are the false pod==================================")
		fmt.Println(falsePodList)
		fmt.Println()

	}
	fmt.Printf("\n%d out of %d pods have problem!\n", falsePodsCnt, podCnt)
}

func FilterDirs(prefix string, dir string) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		currdir := filepath.Join(dir, f.Name())
		currfiles, currerr := ioutil.ReadDir(currdir)
		if currerr != nil {
			return currerr
		}
		for _, nf := range currfiles {
			if !nf.IsDir() && nf.Name() == prefix{
				currLog := filepath.Join(currdir, nf.Name())
				currEventList, currLogEntries, err := readLogs(currLog)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
				switch prefix {
				case "kube-apiserver.log":
					apiEventList = append(apiEventList, currEventList...)
					apiLogEntries = append(apiLogEntries, currLogEntries...)
				case "kube-scheduler.log":
					schedulerEventList = append(schedulerEventList, currEventList...)
				case "kube-controller-manager.log":
					kcmEventList = append(kcmEventList, currEventList...)
				}

			}
		}
	}
	return nil
}

func readLogs(logName string) ( []eventTrackerEntry, []string, error) {
	eventLists, logEntries, err := readLines(logName)
	if err != nil {
		log.Fatalf("Failed reading lines: %v", err)
	}
	return eventLists, logEntries, err
}

func readLines(path string) ( []eventTrackerEntry, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Println()
		return nil, nil, err
	}
	defer file.Close()

	var lines []string
	var eventEntryList []eventTrackerEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		currLine := scanner.Text()
		result := strings.Split(currLine, ",")
		if result[0] == "eventTracker"{
			lines = append(lines, currLine)
			var currEntry eventTrackerEntry
			currEntry.dataPoint = result[1]
			currEntry.timestamp = result[2]
			currEntry.eventType = result[3]
			currEntry.namespace = result[4]
			currEntry.objName   = result[5]
			currEntry.reflectType = result[6]
			currEntry.resourceVersion = result[7]
			eventEntryList = append(eventEntryList, currEntry)
		}
	}
	return eventEntryList, lines, scanner.Err()
}

func getPodsList(apiLogEntries []string) []string {
	fmt.Printf("\nGeting pods list...")
	var pods []string
	for _, line := range apiLogEntries {
		result := strings.Split(line, ",")
		eyeCatcher := result[0]
		if ( eyeCatcher == "eventTracker" ){
			loc := result[1]
			objName := result[5]
			reflectType := result[6]
			if (loc == "watch_cache/processEvent") && ( reflectType == "*core.Pod"){
				pods = AppendPodIfMissing(pods, objName)
			}
		}
	}
	return pods
}

func fillList4APIServer(entryList []eventTrackerEntry, podName string) {
	for _, entry := range entryList {
		// if  strings.Contains(entry.objName, podName) {
		if  entry.objName == podName{
			rv, _ := strconv.Atoi(entry.resourceVersion)
			switch entry.dataPoint {
			case "etcd3/watcher/transform/curObj", "etcd3/watcher/transform/oldObj":
				rvLists[0] = AppendIfMissing(rvLists[0], rv)
				eventLists[0] = append(eventLists[0], entry)
			case "etcd3/watcher/processEvent":
				rvLists[1] = AppendIfMissing(rvLists[1], rv)
				eventLists[1] = append(eventLists[1], entry)
			case "watch_cache/processEvent":
				rvLists[2] = AppendIfMissing(rvLists[2], rv)
				eventLists[2] = append(eventLists[2], entry)
			case "cacher/dispatchEvent":
				rvLists[3] = AppendIfMissing(rvLists[3], rv)
				eventLists[3] = append(eventLists[3], entry)
			case "cacher/add0":
				rvLists[4] = AppendIfMissing(rvLists[4], rv)
				eventLists[4] = append(eventLists[4], entry)
			case "cacher/add1", "cacher/add2", "cacher/add3":
				rvLists[5] = AppendIfMissing(rvLists[5], rv)
				eventLists[5] = append(eventLists[5], entry)
			case "cacher/send0":
				rvLists[6] = AppendIfMissing(rvLists[6], rv)
				eventLists[6] = append(eventLists[6], entry)
			case "cacher/send1":
				rvLists[7] = AppendIfMissing(rvLists[7], rv)
				eventLists[7] = append(eventLists[7], entry)
			case "cacher/send2":
				rvLists[8] = AppendIfMissing(rvLists[8], rv)
				eventLists[8] = append(eventLists[8], entry)
			}
		}
	}
	return
}

func fillList4Client(entryList []eventTrackerEntry, listNum int, podName string) {
	for _, entry := range entryList {
		//if  strings.Contains(entry.objName, podName) {
		if  entry.objName == podName{
			rv, _ := strconv.Atoi(entry.resourceVersion)
			if strings.HasPrefix(entry.dataPoint, "reflector/watchHandler") {
				rvLists[listNum] = AppendIfMissing(rvLists[listNum], rv)
				eventLists[listNum] = append(eventLists[listNum], entry)
			}else{
				fmt.Println("WRONG LOG?!")
			}
		}
	}
	return
}

func compareLists(baseNum int, podName string) ([]int, bool) {
	isSame := true
	baseRVList := rvLists[baseNum]
	baseEventList := eventLists[baseNum]
	var falseList []int
	for i := 0; i < 11; i++ {
		if i == baseNum {
			continue
		}
		issame, falservlist := compareWithBase(baseRVList, rvLists[i])
		if !issame {

			fmt.Printf("List %d is DIFFERENT from base list for pod %s\n", i, podName)
			if len(falseList) == 0 {
				falseList = append(falseList, baseNum)
			}
			falseList = append(falseList, i)
			isSame = false
			switch strings.ToUpper(ecOpts.listtype) {
			case "RV":
				if (ecOpts.podName == "all") || (ecOpts.podName == podName) || (ecOpts.podNameHas != "" && strings.Contains(podName, ecOpts.podNameHas)){
					fmt.Printf("false rv list: ")
					fmt.Println(falservlist)
					//fmt.Println(baseRVList)
					//fmt.Printf("currList %d: \n", i)
					//fmt.Println(rvLists[i])
				}
			case "EVENT":
				if (ecOpts.podName == "all") || (ecOpts.podName == podName) || (ecOpts.podNameHas != "" && strings.Contains(podName, ecOpts.podNameHas)){
					//fmt.Printf("false event list: \n")
					//for _,entry := range falseeventlist {
					//	entry.Print()
					//}
					fmt.Printf("baselist %d: \n", baseNum)
					for _,entry := range baseEventList {
						entry.Print()
					}

					fmt.Printf("currList %d: \n", i)
					for _,entry := range eventLists[i] {
						entry.Print()
					}
				}
			}
		}
	}
	//fmt.Printf("%s, ", podName)
	return falseList,isSame
}

func compareWithBase(baseRVList []int, currRvList []int) (bool, []int){
	isSame := true
	var falservlist []int
	//var falseeventList []eventTrackerEntry
	bl := len(baseRVList)
	cl := len(currRvList)
	if bl != cl {
		fmt.Printf("\nbase rv list has %d events but current rv list has %d events\n", bl, cl)
		isSame = false
	}


	if ecOpts.eventdiff {
		if bl == 0 {
			fmt.Printf("Events missing from base list: \n")
			fmt.Println(currRvList)
			return isSame, currRvList
		}

		if cl == 0 {
			fmt.Printf("Events missing from current list: \n")
			fmt.Println(baseRVList)
			return isSame, baseRVList
		}
	}


	for i, j := 0, 0; i < bl && j < cl; {
		if baseRVList[i] == currRvList[j] {
			i++
			j++
			for (i == bl) && (j < cl) {
				isSame = false
				if ecOpts.eventdiff {
					fmt.Printf("Event %s is missing in base list but exists in current list\n", currRvList[j])
				}
				falservlist = append(falservlist, currRvList[j])
				j++
			}
			for (i < bl) && (j == cl) {
				isSame = false
				if ecOpts.eventdiff {
					fmt.Printf("Event %s is missing in current list but exists in base list\n", baseRVList[i])
				}
				falservlist = append(falservlist, baseRVList[i])
				i++
			}
		} else {
			for baseRVList[i] != currRvList[j] {
				//fmt.Printf("i is %d, j is %d, bl is %d, cl is %d\n", i, j, bl, cl)
				//fmt.Printf("base num is %s, curr num is %s ", baseRVList[i], currRvList[j])

				isSame = false
				if baseRVList[i] < currRvList[j] {
					if ecOpts.eventdiff {
						fmt.Printf("Event %s is missing in current list but exists in base list\n", baseRVList[i])
					}
					falservlist = append(falservlist, baseRVList[i])
					i++
				} else {
					if ecOpts.eventdiff {
						fmt.Printf("Event %s is missing in base list but exists in current list\n", currRvList[j])
					}
					falservlist = append(falservlist, currRvList[j])
					j++
				}
				if i == bl {
					for ;j < cl; j++ {
						if ecOpts.eventdiff {
							fmt.Printf("Event %s is missing in base list but exists in current list\n", currRvList[j])
						}
						falservlist = append(falservlist, currRvList[j])
					}
					return isSame, falservlist
				}
				if j == cl {
					for ;i < bl; i++ {
						if ecOpts.eventdiff {
							fmt.Printf("Event %s is missing in current list but exists in base list\n", baseRVList[i])
						}
						falservlist = append(falservlist, baseRVList[i])
					}
					return isSame, falservlist
				}
			}
		}
	}

	return isSame, falservlist
}


func AppendIfMissing(slice []int, i int) []int {
	for _, ele := range slice {
		if ele == i {
			return slice
		}
	}
	return append(slice, i)
}

func AppendPodIfMissing(slice []string, i string) []string {
	for _, ele := range slice {
		if ele == i {
			return slice
		}
	}
	return append(slice, i)
}



func AppendEventIfMissing(slice []eventTrackerEntry, rvlist []string , i eventTrackerEntry) []eventTrackerEntry {
	for _, rv := range rvlist {
		if rv == i.resourceVersion {
			return slice
		}
	}
	return append(slice, i)
}

func listEntry4FalsePod (podName string, apiEventList []eventTrackerEntry){
	var entries []eventTrackerEntry
	for _, entry := range apiEventList {
		if entry.objName == podName {
			entries = append(entries, entry)
			entry.Print()
		}
	}
	return
}

func listRV4FalsePod (podName string, apiEventList []eventTrackerEntry){
	var rvs []string
	for _, entry := range apiEventList {
		if entry.objName == podName {
			rvs = append(rvs, entry.resourceVersion)
		}
	}
	fmt.Println(rvs)
	return
}

func getNewPodNameList (podNameHas string, falsePodList []string) []string {
	var newPodList []string
	for _,falsePod := range falsePodList {
		if strings.Contains(falsePod, podNameHas){
			newPodList = append(newPodList, falsePod)
		}
	}
	return newPodList
}

func printEvents4FalsePods(falsePodList []string, kasEntryList []eventTrackerEntry, schedulerEntryList []eventTrackerEntry, kcmEntryList []eventTrackerEntry){
	fmt.Println()
	if ecOpts.podName != "" {
		fmt.Printf("\n==================================List events for pod %s==================================\n", ecOpts.podName)
		fmt.Println("\nEvent in apiserver: ")
		listEntry4FalsePod(ecOpts.podName, kasEntryList)
		fmt.Println("\nEvent in scheduler: ")
		listEntry4FalsePod(ecOpts.podName, schedulerEntryList)
		fmt.Println("\nEvent in controller-manager: ")
		listEntry4FalsePod(ecOpts.podName, kcmEntryList)
	} else if ecOpts.podName == "all"{
		for _, podName := range falsePodList {
			fmt.Printf("\n==================================List events for pod %s==================================\n", podName)
			fmt.Println("\nEvent in apiserver: ")
			listEntry4FalsePod(podName, kasEntryList)
			fmt.Println("\nEvent in scheduler: ")
			listEntry4FalsePod(podName, schedulerEntryList)
			fmt.Println("\nEvent in controller-manager: ")
			listEntry4FalsePod(podName, kcmEntryList)
		}
	} else if ecOpts.podNameHas != "" {
		newPodLists := getNewPodNameList(ecOpts.podNameHas, falsePodList)
		for _, podName := range newPodLists {
			fmt.Printf("\n==================================List events for pod %s==================================\n", podName)
			fmt.Println("\nEvent in apiserver: ")
			listEntry4FalsePod(podName, kasEntryList)
			fmt.Println("\nEvent in scheduler: ")
			listEntry4FalsePod(podName, schedulerEntryList)
			fmt.Println("\nEvent in controller-manager: ")
			listEntry4FalsePod(podName, kcmEntryList)
		}
	}
}

func printRV4FalsePods(falsePodList []string, kasEntryList []eventTrackerEntry, schedulerEntryList []eventTrackerEntry, kcmEntryList []eventTrackerEntry){
	fmt.Println()

	if ecOpts.podName != "" {
		fmt.Printf("\n==================================List RV for pod %s==================================\n", ecOpts.podName)
		fmt.Println("\nEvent in apiserver: ")
		listRV4FalsePod(ecOpts.podName, kasEntryList)
		fmt.Println("\nEvent in scheduler: ")
		listRV4FalsePod(ecOpts.podName, schedulerEntryList)
		fmt.Println("\nEvent in controller-manager: ")
		listRV4FalsePod(ecOpts.podName, kcmEntryList)
	} else if ecOpts.podName == "all"{
		for _, podName := range falsePodList {
			fmt.Printf("\n==================================List events for pod %s==================================\n", podName)
			fmt.Println("\nEvent in apiserver: ")
			listRV4FalsePod(podName, kasEntryList)
			fmt.Println("\nEvent in scheduler: ")
			listRV4FalsePod(podName, schedulerEntryList)
			fmt.Println("\nEvent in controller-manager: ")
			listRV4FalsePod(podName, kcmEntryList)
		}
	} else if ecOpts.podNameHas != "" {
		newPodLists := getNewPodNameList(ecOpts.podNameHas, falsePodList)
		for _, podName := range newPodLists {
			fmt.Printf("\n==================================List events for pod %s==================================\n", podName)
			fmt.Println("\nEvent in apiserver: ")
			listRV4FalsePod(podName, kasEntryList)
			fmt.Println("\nEvent in scheduler: ")
			listRV4FalsePod(podName, schedulerEntryList)
			fmt.Println("\nEvent in controller-manager: ")
			listRV4FalsePod(podName, kcmEntryList)
		}
	}
}


