import { HttpClient } from '@angular/common/http';
import { Service, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';
import { CreateTextResponse, Fragment, SearchResult, Text, TextWithSlots } from '../model/text.model';

@Service()
export class TextService {
  private readonly http = inject(HttpClient);

  create(title: string, content: string): Observable<CreateTextResponse> {
    return this.http.post<CreateTextResponse>(`${API_BASE_URL}/api/texts`, { title, content });
  }

  get(id: string): Observable<Text> {
    return this.http.get<Text>(`${API_BASE_URL}/api/texts/${id}`);
  }

  // The most recently created texts, newest first — for the home page's
  // "latest texts" cards.
  listRecent(limit: number): Observable<Text[]> {
    return this.http.get<Text[]>(`${API_BASE_URL}/api/texts`, { params: { limit } });
  }

  // Text + the slots of its current round, if any — one call instead of
  // fetching the text and its round separately, used to restore the
  // colored zones an in-progress text already has when it's reopened.
  getWithSlots(id: string): Observable<TextWithSlots> {
    return this.http.get<TextWithSlots>(`${API_BASE_URL}/api/texts/${id}/with-slots`);
  }

  update(id: string, title: string, content: string): Observable<CreateTextResponse> {
    return this.http.put<CreateTextResponse>(`${API_BASE_URL}/api/texts/${id}`, { title, content });
  }

  search(query: string): Observable<SearchResult[]> {
    return this.http.get<SearchResult[]>(`${API_BASE_URL}/api/texts/search`, { params: { q: query } });
  }

  proposeEdit(textId: string, start: number, end: number, content: string): Observable<Fragment> {
    return this.http.post<Fragment>(`${API_BASE_URL}/api/texts/${textId}/slots`, { start, end, content });
  }

  fragmentsForSlot(textId: string, slotId: string): Observable<Fragment[]> {
    return this.http.get<Fragment[]>(`${API_BASE_URL}/api/texts/${textId}/slots/${slotId}/fragments`);
  }

  getFragment(id: string): Observable<Fragment> {
    return this.http.get<Fragment>(`${API_BASE_URL}/api/fragments/${id}`);
  }

  castVote(fragmentId: string): Observable<unknown> {
    return this.http.post(`${API_BASE_URL}/api/fragments/${fragmentId}/vote`, {});
  }

  closeRound(textId: string): Observable<unknown> {
    return this.http.post(`${API_BASE_URL}/api/texts/${textId}/close-round`, {});
  }

  scheduleClose(textId: string, days: number): Observable<{ scheduledCloseAt: string }> {
    return this.http.post<{ scheduledCloseAt: string }>(`${API_BASE_URL}/api/texts/${textId}/schedule-close`, { days });
  }

  deleteText(id: string): Observable<unknown> {
    return this.http.delete(`${API_BASE_URL}/api/texts/${id}`);
  }

  subscribe(textId: string): Observable<{ subscribed: boolean }> {
    return this.http.post<{ subscribed: boolean }>(`${API_BASE_URL}/api/texts/${textId}/subscribe`, {});
  }
}
