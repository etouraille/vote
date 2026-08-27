import { DatePipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { AppNotification, opensVote } from '../../model/notification.model';
import { NotificationService } from '../../service/notification';

// What push already delivered, readable after the fact.
//
// Its reason for existing is that the web front is notified of nothing in
// real time: no service worker, no push. The inbox is the same events the
// api fanned out, kept server-side, so the browser can catch up on them
// without any of that machinery.
@Component({
  selector: 'notifications-page',
  imports: [DatePipe],
  templateUrl: './notifications.html',
})
export class NotificationsPage implements OnInit {
  private readonly notifications = inject(NotificationService);
  private readonly router = inject(Router);

  readonly items = signal<AppNotification[] | null>(null);
  readonly loadError = signal<string | null>(null);
  readonly marking = signal(false);

  readonly hasUnread = computed(() => this.items()?.some((item) => !item.read) ?? false);

  ngOnInit(): void {
    this.load();
  }

  private load(): void {
    this.notifications.list().subscribe({
      next: (page) => {
        this.items.set(page.notifications);
        this.loadError.set(null);
      },
      error: (err: HttpErrorResponse) => {
        this.loadError.set(err.error?.error ?? 'Chargement impossible.');
      },
    });
  }

  // Reading an entry is what marks it read — no separate gesture, and no
  // way to end up with an inbox full of entries already acted on.
  open(item: AppNotification): void {
    this.setRead(item, true);

    if (!item.textId) return;
    const path = opensVote(item.type) ? '/vote' : '/text';
    this.router.navigate([path], { queryParams: { id: item.textId } });
  }

  // Optimistic: the point of the click is opening the text, and waiting on
  // the round trip would stall that behind a request nobody is watching. A
  // failure rolls the row back so the list never claims a state the server
  // doesn't hold.
  setRead(item: AppNotification, read: boolean): void {
    if (item.read === read) return;

    this.patch(item.id, read);
    this.notifications.setUnread(this.notifications.unread() + (read ? -1 : 1));

    this.notifications.setRead(item.id, read).subscribe({
      error: () => {
        this.patch(item.id, !read);
        this.notifications.setUnread(this.notifications.unread() + (read ? 1 : -1));
      },
    });
  }

  private patch(id: number, read: boolean): void {
    this.items.update(
      (items) => items?.map((item) => (item.id === id ? { ...item, read } : item)) ?? null,
    );
  }

  markAllRead(): void {
    if (this.marking() || !this.hasUnread()) return;

    this.marking.set(true);
    this.notifications.markAllRead().subscribe({
      next: () => {
        this.marking.set(false);
        this.items.update((items) => items?.map((item) => ({ ...item, read: true })) ?? null);
        this.notifications.setUnread(0);
      },
      error: () => {
        // Re-reading beats restoring the old list: a bulk acknowledge may
        // have partly applied, and only the server knows what stuck.
        this.marking.set(false);
        this.load();
      },
    });
  }
}
