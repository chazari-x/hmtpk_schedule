package hmtpk_parser

import (
	"context"

	"github.com/chazari-x/hmtpk_parser/v2/announce"
	"github.com/chazari-x/hmtpk_parser/v2/errors"
	"github.com/chazari-x/hmtpk_parser/v2/schedule"
	"github.com/chazari-x/hmtpk_parser/v2/schedule/group"
	"github.com/chazari-x/hmtpk_parser/v2/schedule/teacher"
	"github.com/chazari-x/hmtpk_parser/v2/storage"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"

	"github.com/chazari-x/hmtpk_parser/v2/model"
)

type Controller struct {
	r        *storage.Redis
	log      *logrus.Logger
	group    *group.Controller
	teacher  *teacher.Controller
	announce *announce.Announce
}

func NewController(client *redis.Client, logger *logrus.Logger) *Controller {
	return &Controller{
		r:        &storage.Redis{Redis: client},
		log:      logger,
		group:    group.NewController(client, logger),
		teacher:  teacher.NewController(client, logger),
		announce: announce.NewAnnounce(logger),
	}
}

// GetScheduleByGroup по идентификатору группы и дате получает расписание на неделю
func (c *Controller) GetScheduleByGroup(ctx context.Context, group, date string) ([]model.Schedule, error) {
	return c.getSchedule(ctx, group, date, c.group)
}

// GetScheduleByTeacher по ФИО преподавателя и дате получает расписание преподавателя
func (c *Controller) GetScheduleByTeacher(ctx context.Context, teacher, date string) ([]model.Schedule, error) {
	return c.getSchedule(ctx, teacher, date, c.teacher)
}

// GetGroupOptions получает список групп
func (c *Controller) GetGroupOptions(ctx context.Context) ([]model.Option, error) {
	return c.group.GetOptions(ctx)
}

// GetTeacherOptions получает список преподавателей
func (c *Controller) GetTeacherOptions(ctx context.Context) ([]model.Option, error) {
	return c.teacher.GetOptions(ctx)
}

func (c *Controller) getSchedule(ctx context.Context, name, date string, adapter schedule.Adapter) ([]model.Schedule, error) {
	if name == "0" || name == "" {
		return nil, errors.ErrorBadRequest
	}

	return adapter.GetSchedule(ctx, name, date)
}

// GetAnnounces получает блок с объявлениями
func (c *Controller) GetAnnounces(ctx context.Context, page int) (model.Announces, error) {
	if page < 1 {
		return model.Announces{}, errors.ErrorBadRequest
	}

	return c.announce.GetAnnounces(ctx, page)
}
