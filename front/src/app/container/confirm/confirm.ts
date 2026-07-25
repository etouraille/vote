import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { AuthService } from '../../service/auth';

@Component({
  selector: 'confirm-page',
  imports: [RouterLink],
  templateUrl: './confirm.html',
})
export class ConfirmPage implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly auth = inject(AuthService);

  readonly status = signal<'pending' | 'success' | 'error'>('pending');
  readonly error = signal<string | null>(null);

  ngOnInit(): void {
    const email = this.route.snapshot.queryParamMap.get('email');
    const code = this.route.snapshot.queryParamMap.get('code');
    if (!email || !code) {
      this.status.set('error');
      this.error.set('Lien de confirmation invalide.');
      return;
    }

    this.auth.confirm(email, code).subscribe({
      next: () => this.status.set('success'),
      error: (err: HttpErrorResponse) => {
        this.status.set('error');
        this.error.set(err.error?.error ?? 'Lien de confirmation invalide ou expiré.');
      },
    });
  }
}
