package model

import (
	"errors"
	"testing"
	"time"
)

func TestOrderStateTransitions(t *testing.T) {
	order := Order{Status: StatusPending}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	if err := order.Transition(StatusProcessing, now); err != nil {
		t.Fatal(err)
	}
	if err := order.Transition(StatusPaid, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if order.PaidAt == nil || order.Status != StatusPaid {
		t.Fatalf("unexpected paid order: %+v", order)
	}
	if err := order.Transition(StatusClosed, now.Add(2*time.Second)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("paid order closed: %v", err)
	}
}

func TestOrderCanCloseOrFailBeforePayment(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []Status{StatusClosed, StatusFailed} {
		order := Order{Status: StatusPending}
		if err := order.Transition(status, now); err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
		if !order.Terminal() {
			t.Fatalf("%s is not terminal", status)
		}
	}
}

func TestMoneyValidation(t *testing.T) {
	for _, amount := range []int64{0, -1} {
		if err := ValidateAmount(amount); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("amount %d accepted", amount)
		}
	}
	if err := ValidateAmount(1); err != nil {
		t.Fatal(err)
	}
}
