import { HttpErrorResponse } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { AuthService } from '../../service/auth';

const PASSWORD_PATTERN = /^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).{8,}$/;

@Component({
  selector: 'register-page',
  imports: [FormsModule, RouterLink],
  templateUrl: './register.html',
})
export class RegisterPage {
  private readonly auth = inject(AuthService);

  email = '';
  pseudo = '';
  password = '';
  confirmPassword = '';
  readonly submitting = signal(false);
  readonly error = signal<string | null>(null);
  readonly registered = signal(false);

  get passwordValid(): boolean {
    return PASSWORD_PATTERN.test(this.password);
  }

  get passwordsMatch(): boolean {
    return this.password.length > 0 && this.password === this.confirmPassword;
  }

  get canSubmit(): boolean {
    return this.passwordValid && this.passwordsMatch && !this.submitting();
  }

  submit(): void {
    if (!this.canSubmit) return;
    this.submitting.set(true);
    this.error.set(null);
    this.auth.register({ email: this.email, password: this.password, pseudo: this.pseudo.trim() }).subscribe({
      next: () => {
        this.submitting.set(false);
        this.registered.set(true);
      },
      error: (err: HttpErrorResponse) => {
        this.submitting.set(false);
        this.error.set(err.error?.error ?? "Erreur lors de l'inscription");
      },
    });
  }
}
