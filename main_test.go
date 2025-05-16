package main

import (
	"log"
	// "net/netip"
	"testing"
	"time"
)



func TestSprintSingleTz(t *testing.T) {

	t.Run("print single tz 3:", func(t *testing.T) {
		t.Parallel()

		strs := []string{"America/Los_Angeles", "America/Los_Angeles", "America/New_York"}
		s := sprintSingleTz(strs, 1001)
		log.Println("print single tz 3:", s)
		if s != "America/Los_Angeles" {
			t.Error()
		}
	})


	t.Run("print single tz 50:", func(t *testing.T) {
		t.Parallel()
		strs := []string{}

		rang := 1000
		if testing.Short() {
			rang = 50
		}

		i := 0
		for len(strs) < rang {
			if i == 0 {
				strs = append(strs, "Asia/Tokyo")
			}
			if i % 2 == 0 {
				strs = append(strs, "America/New_York")
			} else if i % 3 == 0 {
				strs = append(strs, "America/Los_Angeles")
			}

			i++

		}

		st := time.Now()
		s := sprintSingleTz(strs, -1)

		log.Println("print single tz 50:", s)
		log.Printf("print single tz 50: total time: %v per func: %v\n", time.Since(st), time.Since(st) / time.Duration(rang))
		if s != "America/New_York" {
			t.Error()
		}
	})

	t.Run("print single tz 5000:", func(t *testing.T) {
		t.Parallel()
		strs := []string{}

		rang := 5000
		if testing.Short() {
			rang = 50
		}

		i := 0
		for len(strs) < rang {
			if i == 0 {
				strs = append(strs, "Asia/Tokyo")
			}
			if i % 2 == 0 {
				strs = append(strs, "America/New_York")
			} else if i % 3 == 0 {
				strs = append(strs, "America/Los_Angeles")
			}

			i++

		}

		st := time.Now()
		s := sprintSingleTz(strs, -1)

		log.Println("print single tz 5000:", s)
		log.Printf("print single tz 5000: total time: %v per func: %v\n", time.Since(st), time.Since(st) / time.Duration(rang))
		if s != "America/New_York" {
			t.Error()
		}
	})

	t.Run("print single tz 5001:", func(t *testing.T) {
		t.Parallel()
		strs := []string{}

		rang := 5001

		i := 0
		for len(strs) < rang {
			if i == 0 {
				strs = append(strs, "Asia/Tokyo")
			}

			if i % 2 == 0 {
				strs = append(strs, "America/New_York")
			} else if i % 3 == 0 {
				strs = append(strs, "America/Los_Angeles")
			}

			i++

		}

		st := time.Now()
		s := sprintSingleTz(strs, 1001)

		log.Println("print single tz 5001:", s)
		log.Printf("print single tz 5001: total time: %v per func: %v\n", time.Since(st), time.Since(st) / time.Duration(rang))
		if s != "Asia/Tokyo" {
			t.Error()
		}
	})

}
