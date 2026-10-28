package domain

import "time"

// BusinessLocation 是当前产品面向用户展示和持久化的业务时区。
// 数据库使用 DATETIME(3) 没有时区信息，因此写入和读取必须统一使用 Asia/Shanghai，避免同一条 Lesson 前后相差 8 小时。
var BusinessLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func Now() time.Time {
	return time.Now().In(BusinessLocation)
}
