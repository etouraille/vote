import { DecimalPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { SearchResult } from '../../model/text.model';
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
  private readonly router = inject(Router);
  private readonly textService = inject(TextService);

  searchQuery = '';

  readonly searching = signal(false);
  readonly searchResults = signal<SearchResult[] | null>(null);
  readonly searchError = signal<string | null>(null);

  readonly recentTexts = signal<RecentTextCard[]>([]);

  ngOnInit(): void {
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
}
