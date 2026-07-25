import { Component, inject } from "@angular/core";
import { CounterState } from "../service/counter-state";

@Component({
  selector: 'test-counter',
  template : `<div>{{ count() }}</div><button (click)="increment()">More</button>`
})
export class TestCounter {
  state = inject(CounterState);
  count = this.state.count; // can read but not modify
  increment() {
    this.state.increment();
  }
}