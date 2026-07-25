import { DecimalPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { SearchResult } from '../../model/text.model';
import { AuthService } from '../../service/auth';
import { TextService } from '../../service/text';
import { firstWords } from '../../util/words';

const RECENT_TEXTS_COUNT = 4;
const EXCERPT_WORD_COUNT = 100;

export interface RecentTextCard {
  id: string;
  title: string;
  excerpt: string;
}

@Component({
  selector: 'home-page',
  imports: [FormsModule, DecimalPipe, RouterLink],
  templateUrl: './home.html',
})
export class HomePage implements OnInit {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly textService = inject(TextService);

  title = '';
  searchQuery = '';

  readonly searching = signal(false);
  readonly searchResults = signal<SearchResult[] | null>(null);
  readonly searchError = signal<string | null>(null);

  // Only known once /api/me answers — the create-text form stays hidden
  // until then, rather than flashing and disappearing if the user turns
  // out not to have the right.
  readonly canCreateText = signal(false);

  readonly recentTexts = signal<RecentTextCard[]>([]);

  ngOnInit(): void {
    this.auth.me().subscribe((me) => {
      this.canCreateText.set(me.root || me.permissions.canCreateText);
    });
    this.loadRecentTexts();
  }

  private loadRecentTexts(): void {
    this.textService.listRecent(RECENT_TEXTS_COUNT).subscribe((texts) => {
      this.recentTexts.set(
        texts.map((text) => ({
          id: text.id,
          title: text.title,
          excerpt: firstWords(text.content, 0, EXCERPT_WORD_COUNT),
        })),
      );
    });
  }

  search(): void {
    const query = this.searchQuery.trim();
    if (!query) return;

    this.searching.set(true);
    this.searchError.set(null);
    this.textService.search(query).subscribe({
      next: (results) => {
        this.searching.set(false);
        this.searchResults.set(results);
      },
      error: (err: HttpErrorResponse) => {
        this.searching.set(false);
        this.searchError.set(err.error?.error ?? 'Erreur lors de la recherche');
      },
    });
  }

  openResult(result: SearchResult): void {
    this.router.navigate(['/editor'], { queryParams: { id: result.textId } });
  }

  logout(): void {
    this.auth.logout();
    this.router.navigateByUrl('/');
  }

  createText(): void {
    const title = this.title.trim();
    if (!title) return;
    this.router.navigate(['/editor'], { queryParams: { title } });
  }
}
