import { Routes } from '@angular/router';
import { LoginPage } from './container/login/login';
import { authGuard } from './guard/auth-guard';
import { adminGuard } from './guard/admin-guard';

export const routes: Routes = [
  { path: '', component: LoginPage, title: 'Connexion' },
  {
    path: 'register',
    loadComponent: () => import('./container/register/register').then((m) => m.RegisterPage),
    title: 'Inscription',
  },
  {
    path: 'confirm',
    loadComponent: () => import('./container/confirm/confirm').then((m) => m.ConfirmPage),
    title: 'Validation du compte',
  },
  {
    path: 'home',
    canActivate: [authGuard],
    loadComponent: () => import('./container/home/home').then((m) => m.HomePage),
    title: 'Accueil',
  },
  {
    path: 'editor',
    canActivate: [authGuard],
    loadComponent: () => import('./container/editor/editor').then((m) => m.EditorPage),
    title: 'Editeur',
  },
  {
    path: 'backoffice',
    canActivate: [adminGuard],
    loadComponent: () => import('./container/backoffice/backoffice').then((m) => m.BackofficePage),
    title: 'Backoffice',
  },
  {
    path: 'vote',
    canActivate: [authGuard],
    loadComponent: () => import('./container/vote/vote').then((m) => m.VotePage),
    title: 'Voter',
  },
  {
    path: 'text',
    canActivate: [authGuard],
    loadComponent: () => import('./container/text/text').then((m) => m.TextPage),
    title: 'Texte',
  },
  { path: '**', redirectTo: '' },
];
