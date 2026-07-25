import { HttpErrorResponse } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { AuthService } from '../../service/auth';

@Component({
  selector: 'login-page',
  imports: [FormsModule, RouterLink],
  templateUrl: './login.html',
})
export class LoginPage {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);

  email = '';
  password = '';
  readonly submitting = signal(false);
  readonly error = signal<string | null>(null);

  constructor() {
    if (this.auth.isAuthenticated()) {
      this.router.navigateByUrl('/home');
    }
  }

  submit(): void {
    if (this.submitting()) return;
    this.submitting.set(true);
    this.error.set(null);
    this.auth.login({ email: this.email, password: this.password }).subscribe({
      next: () => {
        this.submitting.set(false);
        this.router.navigateByUrl('/home');
      },
      error: (err: HttpErrorResponse) => {
        this.submitting.set(false);
        this.error.set(err.error?.error ?? 'Email ou mot de passe invalide');
      },
    });
  }
}
