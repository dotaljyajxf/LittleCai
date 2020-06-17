package cache

import (
	"backend/conf"
	"errors"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
)

type DbCache struct {
	conn_pool *redis.Pool
}

func InitDbCache() *DbCache {
	dc := new(DbCache)
	dc.initPool()
	return dc
}

func (c *DbCache) initPool() {
	c.conn_pool = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial(
				"tcp",
				fmt.Sprintf(conf.Config.CacheRedisHost),
				//redis.DialPassword(cfg.Password),
				redis.DialDatabase(conf.Config.CacheRedisDB),
				redis.DialConnectTimeout(time.Second*2),
				redis.DialReadTimeout(time.Second*2),
				redis.DialWriteTimeout(time.Second*2),
			)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
		MaxIdle:     conf.Config.CacheRedisMaxIdel,
		MaxActive:   conf.Config.CacheRedisMaxActive,
		IdleTimeout: time.Duration(conf.Config.CacheRedisIdelTimeout),
		Wait:        true,
	}

	if _, err := c.do("PING"); err != nil {
		panic(err)
	}
}

func (c *DbCache) do(command string, args ...interface{}) (reply interface{}, err error) {
	if c.conn_pool == nil {
		return nil, errors.New("NotInitRedis")
	}
	conn := c.conn_pool.Get()
	defer conn.Close()
	return conn.Do(command, args)
}

func (c *DbCache) Get(key string) (reply interface{}, err error) {
	return c.do("GET", key)
}

func (c *DbCache) Set(key string, val interface{}) (reply interface{}, err error) {
	return c.do("SET", key, val)
}

func (c *DbCache) Del(key string) (reply interface{}, err error) {
	return c.do("Del", key)
}
