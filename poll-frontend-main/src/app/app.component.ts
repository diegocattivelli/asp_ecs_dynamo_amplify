import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { PollService, Poll } from './poll.service';

@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.component.html',
  styleUrl: './app.component.css',
})
export class AppComponent {
  private pollService = inject(PollService);

  poll = signal<Poll | null>(null);
  loading = signal(true);
  errorMessage = signal('');

  totalVotes = computed(() => this.poll()?.options.reduce((acc, o) => acc + o.votes, 0) ?? 0);

  constructor() {
    this.loadPoll();
  }

  loadPoll(): void {
    this.loading.set(true);
    this.errorMessage.set('');
    this.pollService.getPoll().subscribe({
      next: (poll) => {
        this.poll.set(poll);
        this.loading.set(false);
      },
      error: () => {
        this.errorMessage.set('No se pudo cargar la encuesta.');
        this.loading.set(false);
      },
    });
  }

  vote(optionId: string): void {
    this.pollService.vote(optionId).subscribe({
      next: (poll) => this.poll.set(poll),
      error: () => this.errorMessage.set('No se pudo registrar el voto.'),
    });
  }

  reset(): void {
    this.pollService.reset().subscribe({
      next: (poll) => this.poll.set(poll),
      error: () => this.errorMessage.set('No se pudo reiniciar la encuesta.'),
    });
  }

  percentage(votes: number): number {
    const total = this.totalVotes();
    return total === 0 ? 0 : Math.round((votes / total) * 100);
  }
}
