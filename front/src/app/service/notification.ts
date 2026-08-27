import { HttpClient, HttpParams } from '@angular/common/http';
import { Service, inject, signal } from '@angular/core';
import { Observable } from 'rxjs';
import { tap } from 'rxjs/operators';
import { API_BASE_URL } from '../api-base-url';
import { NotificationPage } from '../model/notification.model';

// Asking for a single entry: the api answers with the unread count over
// the whole inbox whatever the page size, so refreshing the badge costs
// one row rather than fifty, and there is no separate count route to keep
// in step with the listing one.
const BADGE_PAGE_SIZE = 1;

@Service()
export class NotificationService {
  private readonly http = inject(HttpClient);

  // The badge, shared by whoever displays it. Held here rather than in the
  // shell component so the inbox page can correct it from its own listing
  // without the two drifting apart.
  private readonly _unread = signal(0);
  readonly unread = this._unread.asReadonly();

  // Every read of the inbox refreshes the badge from the same response —
  // the two can't disagree if they come from one read.
  list(limit?: number): Observable<NotificationPage> {
    // HttpParams rather than an object literal: a `{} | { limit: number }`
    // union makes TypeScript pick the wrong get() overload and infer an
    // ArrayBuffer response.
    let params = new HttpParams();
    if (limit !== undefined) params = params.set('limit', limit);

    return this.http
      .get<NotificationPage>(`${API_BASE_URL}/api/me/notifications`, { params })
      .pipe(tap((page) => this._unread.set(page.unread)));
  }

  // Called by the shell on load and after the inbox is left. Errors are
  // swallowed: a badge that failed to refresh must never surface over
  // whatever the user was actually doing.
  refreshUnread(): void {
    this.list(BADGE_PAGE_SIZE).subscribe({ error: () => {} });
  }

  // Both directions through the same route, so an inbox stays revisitable.
  setRead(id: number, read: boolean): Observable<void> {
    return this.http.put<void>(`${API_BASE_URL}/api/me/notifications/${id}/read`, { read });
  }

  markAllRead(): Observable<{ updated: number }> {
    return this.http.post<{ updated: number }>(`${API_BASE_URL}/api/me/notifications/read-all`, {});
  }

  // Applied locally by the inbox, which already knows the exact figure and
  // shouldn't make the server say it twice.
  setUnread(value: number): void {
    this._unread.set(Math.max(0, value));
  }

  // Signing out must clear it, or the next person to sign in in this tab
  // inherits a badge counting someone else's notifications.
  clear(): void {
    this._unread.set(0);
  }
}
