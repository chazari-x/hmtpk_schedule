package group

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	firstDayNum  = 2
	lastDayNum   = firstDayNum + 6
	numOfColumns = 5
)

func (c *Controller) GetSchedule(name, date string, ctx context.Context) ([]model.Schedule, error) {
	var weeklySchedule []model.Schedule

	c.log.Trace(name)

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

	href := fmt.Sprintf("https://hmtpk.ru/ru/students/schedule/?group=%s&date_edu1c=%s&send=Показать#current", name, date)
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
		if c.r.Redis != nil {
			if marshal, err := json.Marshal(weeklySchedule); err == nil {
				if err = c.r.Set(fmt.Sprintf("%d/%d", year, week)+":"+name, string(marshal)); err != nil {
					c.log.Error(err)
				} else {
					c.log.Trace("Данные сохранены в redis")
				}
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
		Href: fmt.Sprintf("https://hmtpk.ru/ru/students/schedule/?group=%s&date_edu1c=%s&send=Показать#current", name, date),
	}

	var before string

	lessonsElement := doc.Children().Find(fmt.Sprintf("div.raspcontent.m5 div:nth-child(%d) div.panel-body > #mobile-friendly > tbody:nth-child(2)", scheduleElementNum))
	for lessonNum := 1; lessonNum > 0; lessonNum++ {
		if len(schedule.Lessons) > 0 {
			before = schedule.Lessons[len(schedule.Lessons)-1].Num
		}

		if lesson, exists := c.parseLesson(lessonsElement, lessonNum, before); exists {
			schedule.Lessons = append(schedule.Lessons, lesson)
		} else {
			break
		}
	}

	return schedule
}

func (c *Controller) parseLesson(lessonsElement *goquery.Selection, lessonNum int, before string) (model.Lesson, bool) {
	var lesson model.Lesson
	var exists bool
	lessonElement := lessonsElement.Find(fmt.Sprintf("tr:nth-child(%d)", lessonNum))
	for lessonAttributeNum := 1; lessonAttributeNum <= numOfColumns; lessonAttributeNum++ {
		lesson, exists = c.parseLessonAttribute(lessonElement, lessonAttributeNum, lesson, before)
		if !exists {
			break
		}
	}

	return lesson, exists
}

func (c *Controller) parseLessonAttribute(lessonElement *goquery.Selection, lessonAttributeNum int, lesson model.Lesson, before string) (model.Lesson, bool) {
	lessonElementAttribute := lessonElement.Find(fmt.Sprintf("td:nth-child(%d)", lessonAttributeNum))
	value, exists := lessonElementAttribute.Attr("data-title")
	if !exists {
		if lessonAttributeNum == numOfColumns {
			return lesson, true
		} else if lessonAttributeNum == 1 {
			return lesson, false
		}
	}

	text := lessonElementAttribute.Text()
	switch value {
	case "Номер урока":
		lesson.Num = text
	case "Время":
		if lesson.Num == "" {
			lesson.Num = before
		}
		lesson.Time = text
	case "Название предмета":
		if strings.HasSuffix(text, "(1)") || strings.HasSuffix(text, "(2)") {
			switch text[len(text)-3:] {
			case "(1)":
				lesson.Subgroup = "1"
			case "(2)":
				lesson.Subgroup = "2"
			}
			lesson.Name = strings.TrimSpace(strings.TrimRight(strings.TrimRight(text, " (2)"), " (1)"))
		} else {
			lesson.Name = strings.TrimSpace(text)
		}
	case "Кабинет":
		lesson.Room = text
	case "Преподаватель":
		lesson.Teacher = text
	}

	return lesson, true
}
