import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { Text } from '../../model/text.model';
import { TextService } from '../../service/text';

// Read-only view of a single text, opened by clicking a search result's
// title on the home page. Deliberately offers nothing else: subscribing,
// editing, voting and closing all stay on the result row itself, so this
// page is only ever "let me actually read this one".
@Component({
  selector: 'text-page',
  imports: [RouterLink],
  templateUrl: './text.html',
})
export class TextPage implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly textService = inject(TextService);

  readonly text = signal<Text | null>(null);
  readonly loading = signal(true);
  readonly loadError = signal<string | null>(null);

  ngOnInit(): void {
    const id = this.route.snapshot.queryParamMap.get('id');
    if (!id) {
      this.loading.set(false);
      this.loadError.set("Aucun id de texte fourni dans l'url (?id=...).");
      return;
    }

    this.textService.get(id).subscribe({
      next: (text) => {
        this.text.set(text);
        this.loading.set(false);
      },
      error: (err: HttpErrorResponse) => {
        this.loading.set(false);
        this.loadError.set(err.status === 404 ? "Ce texte n'existe pas ou plus." : 'Chargement impossible.');
      },
    });
  }
}
