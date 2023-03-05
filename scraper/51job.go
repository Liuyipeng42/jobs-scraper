package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"job"
	"log"
	"strings"
	"sync"
	"task"
	"time"
	"utils"

	"github.com/nsqio/go-nsq"
	"github.com/tebeka/selenium"
)

func switchCity(chrome utils.ChromeSerADri, city int) {

	allCity := chrome.WaitAndFindOne("div.allcity", 10, 1)
	allCity.Click()

	fliter := chrome.WaitAndFindOne("div.j_filter", 5, 1)
	tables, _ := fliter.FindElements(selenium.ByCSSSelector, "div[role=dialog]")
	table := tables[0]

	tagTbale, _ := table.FindElement(selenium.ByCSSSelector, "div.tags-text")
	tags, _ := tagTbale.FindElements(selenium.ByCSSSelector, "i")

	for i := 0; i < len(tags); i++ {
		tags[i].Click()
	}

	// time.Sleep(2000 * time.Millisecond)
	citys, _ := table.FindElements(selenium.ByCSSSelector, "div.grid-item>span")
	citys[city].Click()

	yes, _ := table.FindElement(selenium.ByCSSSelector, "div.el-dialog__footer>span")
	yes.Click()

}

func switchJobType(chrome utils.ChromeSerADri, jobId int) {

	selectType := chrome.WaitAndFindOne("div.e_e.e_com", 10, 1)
	selectType.Click()

	fliter := chrome.WaitAndFindOne("div.j_filter", 5, 1)
	tables, _ := fliter.FindElements(selenium.ByCSSSelector, "div[role=dialog]")
	table := tables[1]

	// 清除已有的 tag
	tagTbale, _ := table.FindElement(selenium.ByCSSSelector, "div.tags-text")
	time.Sleep(1 * time.Second)
	tags, _ := tagTbale.FindElements(selenium.ByCSSSelector, "span>i")
	for i := 0; i < len(tags); i++ {
		tags[i].Click()
	}

	// 找到 jobId 的对应的 type 
	tabList, _ := table.FindElements(selenium.ByCSSSelector, "div[role=tablist]>div")
	panelList, _ := table.FindElements(selenium.ByCSSSelector, "div[role=tabpanel]")

	tabLen := []int{15, 19, 24, 37, 42, 49, 52, 56, 59, 62, 70, 75}
	
	var tab selenium.WebElement
	var panel selenium.WebElement
	relativeId := jobId

	for i := 0; i < len(tabLen); i++ {
		if jobId - tabLen[i] < 0 {
			tab = tabList[i]
			panel = panelList[i]
			if i > 0{
				relativeId = jobId - tabLen[i - 1]
			}
			break
		}
	}

	tab.Click()
	jobTypes, _ := panel.FindElements(selenium.ByCSSSelector, "div.table-body-tr-td>span")
	jobTypes[relativeId].Click()
	all, _ := panel.FindElement(selenium.ByCSSSelector, "div.clickAll>span")
	all.Click()
	yes, _ := table.FindElement(selenium.ByCSSSelector, "div.el-dialog__footer>span")
	yes.Click()
	search, _ := chrome.Webdriver.FindElement(selenium.ByCSSSelector, "button#search_btn")
	search.Click()

}

func parser(html string) (j job.Job) {

	name := utils.RegExpFindOne(html, "title=.*?class=\"jname at\"")
	j.Name = name[7 : len(name)-18]

	jobInfo := strings.Split(utils.RegExpFindOne(html, "class=\"info\">.*?</p>"), "<span")
	j.Salary = jobInfo[1][32 : len(jobInfo[1])-7]
	j.Position = jobInfo[3][20 : len(jobInfo[3])-7]
	j.Position = strings.ReplaceAll(j.Position, "·", "-")
	if len(jobInfo) > 5 {
		j.Experience = jobInfo[5][20 : len(jobInfo[5])-7]
		if len(jobInfo) > 7 {
			j.Degree = jobInfo[7][20 : len(jobInfo[7])-18]
		}
	}

	tagElems := strings.Split(utils.RegExpFindOne(html, "class=\"tags\">.*?</p>"), "title=")[1:]

	var tagBuffer bytes.Buffer
	for _, tag := range tagElems {
		tagBuffer.WriteString(tag[1:strings.Index(tag, ">")-1] + " ")
	}
	j.Tags = tagBuffer.String()
	j.Tags = j.Tags[:len(j.Tags)]

	url := utils.RegExpFindOne(html, "<a .*? href=\".*?\" target=\"_blank\" class=\"el\">")
	j.Url = url[strings.Index(url, "http") : len(url)-29]

	cname := utils.RegExpFindOne(html, "class=\"cname at\">.*?</a>")
	j.CName = cname[17 : len(cname)-4]

	companyInfo := utils.RegExpFindOne(html, "class=\"dc at\">.*?</p>")
	CInfo := strings.Split(companyInfo[14:len(companyInfo)-4], " | ")
	j.Company.CType = CInfo[0]
	if len(CInfo) > 1 {
		j.Company.CSize = CInfo[1]
	}

	business := utils.RegExpFindOne(html, "class=\"int at\">.*?</p>")
	j.MainBusiness = strings.ReplaceAll(business[15:len(business)-4], "/", " ")

	return
}

func sendJobData(chrome utils.ChromeSerADri, producer *nsq.Producer, j task.Job) {

	// send error msg
	defer func() {

	}()

	chrome.Webdriver.Get("https://we.51job.com/pc/search")

	switchCity(chrome, j.CityId)

	switchJobType(chrome, j.TypeId)

	for page := j.PageStart; page < j.PageEnd; page++ {
		jobs := chrome.WaitAndFindAll("div.j_joblist>div[sensorsname]", 5)
		fmt.Printf("city: %d, type: %d page: %d jobs: %d\n", j.CityId, j.TypeId, page, len(jobs))

		for _, job := range jobs {
			html, _ := job.GetAttribute("outerHTML")
			job := parser(html)
			fmt.Println(job.Name)

			jobJson, _ := json.Marshal(job)
			if err := producer.Publish("jobs", jobJson); err != nil {
				log.Fatal("publish error: " + err.Error())
			}
		}

		input := chrome.WaitAndFindOne("input#jump_page", 2, 1)
		input.Clear()
		input.SendKeys(fmt.Sprintf("%d", page+1))
		button, _ := chrome.Webdriver.FindElement(selenium.ByCSSSelector, "span.jumpPage")
		button.Click()
	}
}

func startConsumer(topic, channel string) {
	consumer, err := nsq.NewConsumer(topic, channel, nsq.NewConfig())
	if err != nil {
		log.Fatal(err)
	}
	consumer.AddHandler(nsq.HandlerFunc(handler))
	if err := consumer.ConnectToNSQD("localhost:4150"); err != nil {
		log.Fatal(err)
	}
	<-consumer.StopChan
}

func handler(message *nsq.Message) error {

	var wg sync.WaitGroup
	t := task.Task{}
	json.Unmarshal(message.Body, &t)
	fmt.Println(t)

	for i := 0; i < len(t.Jobs); i++ {
		wg.Add(1)
		go func(j task.Job) {
			startScrape(j)
			wg.Done()
		}(t.Jobs[i])

		if (i+1)%t.Goroutines == 0 {
			wg.Wait()
		}
	}

	return nil
}

func startScrape(j task.Job) {

	producer, err := nsq.NewProducer("localhost:4150", nsq.NewConfig())
	if err != nil {
		log.Fatal(err)
	}

	chrome := utils.InitClientByRemote("http://localhost:4444/wd/hub")
	defer chrome.Service.Stop()
	defer chrome.Webdriver.Quit()

	sendJobData(chrome, producer, j)
}

func main() {
	startConsumer("task_list1", "task_channel")

	// chrome := utils.InitClientByDriver("./chromedriver", 8080, false)
	// defer chrome.Webdriver.Close()
	// defer chrome.Service.Stop()

	// sendJobData(chrome, nil, task.Job{CityId: 0, TypeId: 1, PageStart: 0, PageEnd: 1})
}