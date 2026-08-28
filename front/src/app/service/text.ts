import { HttpClient, HttpParams } from '@angular/common/http';
import { Service, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';
import {
  CreateTextResponse,
  Fragment,
  HistoryVersion,
  RoundOutcome,
  RecentText,
  SearchResult,
  SubscribedText,
  TagCount,
  Text,
  TextWithSlots,
} from '../model/text.model';

@Service()
export class TextService {
  private readonly http = inject(HttpClient);

  // tags is the author's line as typed, "#"-separated. Sent raw: the api
  // parses it, so every client files a text under the same labels.
  create(title: string, content: string, tags = ''): Observable<CreateTextResponse> {
    return this.http.post<CreateTextResponse>(`${API_BASE_URL}/api/texts`, {
      title,
      content,
      tags,
    });
  }

  get(id: string): Observable<Text> {
    return this.http.get<Text>(`${API_BASE_URL}/api/texts/${id}`);
  }

  // The most recently created texts, newest first — for the home page's
  // "latest texts" cards. offset paginates for infinite scroll: each call
  // asks for the next `limit` texts after however many are already loaded.
  //
  // tag narrows the same listing rather than calling elsewhere: the caller
  // wants the recent texts, filtered, and the answer has the same shape.
  listRecent(limit: number, offset = 0, tag = ''): Observable<RecentText[]> {
    let params = new HttpParams().set('limit', limit).set('offset', offset);
    if (tag !== '') params = params.set('tag', tag);
    return this.http.get<RecentText[]>(`${API_BASE_URL}/api/texts`, { params });
  }

  // The labels in use, most used first — what the filter offers.
  tags(): Observable<TagCount[]> {
    return this.http.get<TagCount[]>(`${API_BASE_URL}/api/tags`);
  }

  // Text + the slots of its current round, if any — one call instead of
  // fetching the text and its round separately, used to restore the
  // colored zones an in-progress text already has when it's reopened.
  getWithSlots(id: string): Observable<TextWithSlots> {
    return this.http.get<TextWithSlots>(`${API_BASE_URL}/api/texts/${id}/with-slots`);
  }

  // Every version of a text, oldest first, each with the rounds that ran
  // on it. Any version's id works: the api walks the chain both ways.
  history(id: string): Observable<HistoryVersion[]> {
    return this.http.get<HistoryVersion[]>(`${API_BASE_URL}/api/texts/${id}/history`);
  }

  search(query: string): Observable<SearchResult[]> {
    return this.http.get<SearchResult[]>(`${API_BASE_URL}/api/texts/search`, {
      params: { q: query },
    });
  }

  proposeEdit(textId: string, start: number, end: number, content: string): Observable<Fragment> {
    return this.http.post<Fragment>(`${API_BASE_URL}/api/texts/${textId}/slots`, {
      start,
      end,
      content,
    });
  }

  fragmentsForSlot(textId: string, slotId: string): Observable<Fragment[]> {
    return this.http.get<Fragment[]>(
      `${API_BASE_URL}/api/texts/${textId}/slots/${slotId}/fragments`,
    );
  }

  getFragment(id: string): Observable<Fragment> {
    return this.http.get<Fragment>(`${API_BASE_URL}/api/fragments/${id}`);
  }

  // Which fragment the caller currently has voted for in each slot of a
  // text, keyed by slot id — slots they haven't voted in are absent. Lets
  // the vote page show a choice made in an earlier session, which the
  // fragments listing alone can't tell it.
  myVotes(textId: string): Observable<Record<string, string>> {
    return this.http.get<Record<string, string>>(`${API_BASE_URL}/api/texts/${textId}/my-votes`);
  }

  castVote(fragmentId: string): Observable<unknown> {
    return this.http.post(`${API_BASE_URL}/api/fragments/${fragmentId}/vote`, {});
  }

  // Answers with the new version the close produced, not just an ack — the
  // caller is showing the text that was closed and needs to move on to the
  // one that replaced it.
  closeRound(textId: string): Observable<RoundOutcome> {
    return this.http.post<RoundOutcome>(`${API_BASE_URL}/api/texts/${textId}/close-round`, {});
  }

  scheduleClose(textId: string, days: number): Observable<{ scheduledCloseAt: string }> {
    return this.http.post<{ scheduledCloseAt: string }>(
      `${API_BASE_URL}/api/texts/${textId}/schedule-close`,
      { days },
    );
  }

  deleteText(id: string): Observable<unknown> {
    return this.http.delete(`${API_BASE_URL}/api/texts/${id}`);
  }

  subscribe(textId: string): Observable<{ subscribed: boolean }> {
    return this.http.post<{ subscribed: boolean }>(
      `${API_BASE_URL}/api/texts/${textId}/subscribe`,
      {},
    );
  }

  // The texts the caller follows, newest subscription first. The caller is
  // taken from the bearer token, so nothing identifies them in the url.
  subscriptions(): Observable<SubscribedText[]> {
    return this.http.get<SubscribedText[]>(`${API_BASE_URL}/api/me/subscriptions`);
  }
}
