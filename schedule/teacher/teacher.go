package teacher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chazari-x/hmtpk_schedule/model"
	"github.com/chazari-x/hmtpk_schedule/storage"
	"github.com/chazari-x/hmtpk_schedule/utils"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

type Controller struct {
	r   *storage.Redis
	log *logrus.Logger
}

func NewController(client *redis.Client, logger *logrus.Logger) *Controller {
	return &Controller{r: &storage.Redis{Redis: client}, log: logger}
}

const (
	firstDayNum  = 1
	lastDayNum   = firstDayNum + 6
	numOfColumns = 5
)

func (c *Controller) GetSchedule(name, date string, ctx context.Context) ([]model.Schedule, error) {
	var weeklySchedule []model.Schedule

	c.log.Trace(name)

	name = strings.ReplaceAll(name, " ", "+")
	d, err := time.Parse("02.01.2006", date)
	if err != nil {
		return nil, err
	}

	year, week := d.ISOWeek()
	if utils.RedisIsNil(c.r) {
		if redisWeeklySchedule, err := c.r.Get(fmt.Sprintf("%d/%d", year, week) + ":" + name); err == nil && redisWeeklySchedule != "" {
			if json.Unmarshal([]byte(redisWeeklySchedule), &weeklySchedule) == nil {
				c.log.Trace("Данные получены из redis")
				return weeklySchedule, nil
			}
		}
	}

	href := fmt.Sprintf("https://hmtpk.ru/ru/teachers/schedule/?teacher=%s&date_edu1c=%s&send=Показать#current", name, date)
	request, err := http.NewRequestWithContext(ctx, "POST", href, nil)
	if err != nil {
		return nil, err
	}

	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("Ошибка: %s", resp.Status))
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	for scheduleElementNum := firstDayNum; scheduleElementNum <= lastDayNum; scheduleElementNum++ {
		weeklySchedule = append(weeklySchedule, c.parseDay(doc, scheduleElementNum, name))
	}

	if utils.RedisIsNil(c.r) {
		if marshal, err := json.Marshal(weeklySchedule); err == nil {
			if err := c.r.Set(fmt.Sprintf("%d/%d", year, week)+":"+name, string(marshal)); err != nil {
				c.log.Error(err)
			} else {
				c.log.Trace("Данные сохранены в redis")
			}
		}
	}

	return weeklySchedule, nil
}

func (c *Controller) parseDay(doc *goquery.Document, scheduleElementNum int, name string) model.Schedule {
	scheduleDateElement := doc.Children().Find(fmt.Sprintf("div.raspcontent.m5 div:nth-child(%d) div.panel-heading.edu_today > h2", scheduleElementNum))

	date := utils.GetDate(strings.Split(scheduleDateElement.Text(), ",")[0])
	var schedule = model.Schedule{
		Date: scheduleDateElement.Text(),
		Href: fmt.Sprintf("https://hmtpk.ru/ru/teachers/schedule/?teacher=%s&date_edu1c=%s&send=Показать#current", name, date),
	}

	lessonsElement := doc.Children().Find(fmt.Sprintf("div.raspcontent.m5 div:nth-child(%d) div.panel-body > table.table > tbody:nth-child(2)", scheduleElementNum))
	for lessonNum := 1; lessonNum > 0; lessonNum++ {
		if lesson, exists := c.parseLesson(lessonsElement, lessonNum, ""); exists {
			schedule.Lessons = append(schedule.Lessons, lesson)
		} else {
			break
		}
	}

	return schedule
}

func (c *Controller) parseLesson(lessonsElement *goquery.Selection, lessonNum int, _ string) (model.Lesson, bool) {
	var lesson model.Lesson
	var exists bool
	lessonElement := lessonsElement.Find(fmt.Sprintf("tr:nth-child(%d)", lessonNum))
	for lessonAttributeNum := 1; lessonAttributeNum <= numOfColumns; lessonAttributeNum++ {
		lesson, exists = c.parseLessonAttribute(lessonElement, lessonAttributeNum, lesson, "")
		if !exists {
			break
		}
	}

	return lesson, exists
}

func (c *Controller) parseLessonAttribute(lessonElement *goquery.Selection, lessonAttributeNum int, lesson model.Lesson, _ string) (model.Lesson, bool) {
	lessonElementAttribute := lessonElement.Find(fmt.Sprintf("td:nth-child(%d)", lessonAttributeNum))
	value := lessonElementAttribute.Text()
	if value == "" {
		return lesson, lessonAttributeNum != 1
	}

	value = strings.ReplaceAll(value, "\n", "")
	value = strings.TrimSpace(value)
	switch lessonAttributeNum {
	case 1:
		lesson.Num = value
	case 2:
		lesson.Time = value
	case 3:
		if strings.HasSuffix(value, "(1)") || strings.HasSuffix(value, "(2)") {
			switch value[len(value)-3:] {
			case "(1)":
				lesson.Subgroup = "1"
			case "(2)":
				lesson.Subgroup = "2"
			}
			lesson.Name = strings.TrimRight(strings.TrimRight(value, " (2)"), " (1)")
		} else {
			lesson.Name = value
		}
	case 4:
		lesson.Group = value
	case 5:
		room := strings.TrimSpace(regexp.MustCompile("\\W-[0-9]{1,3}$").FindString(value))
		if room == "" {
			lesson.Room = strings.TrimSpace(value)
		} else {
			lesson.Room = room
			lesson.Location = strings.TrimSpace(strings.TrimRight(value, room))
		}
	}

	return lesson, true
}
