import { Service, signal } from "@angular/core";


@Service()
export class CounterState {
  // Private writable state
  private readonly _count = signal(0);
  readonly count = this._count.asReadonly(); // public readonly
  increment() {
    this._count.update((v) => v + 1);
  }
}