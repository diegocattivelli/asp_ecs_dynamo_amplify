import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '../environments/environment';

export interface Option {
  id: string;
  text: string;
  votes: number;
}

export interface Poll {
  question: string;
  options: Option[];
}

@Injectable({ providedIn: 'root' })
export class PollService {
  private http = inject(HttpClient);
  private baseUrl = `${environment.apiUrl}/api/poll`;

  getPoll(): Observable<Poll> {
    return this.http.get<Poll>(this.baseUrl);
  }

  vote(optionId: string): Observable<Poll> {
    return this.http.post<Poll>(`${this.baseUrl}/vote`, { optionId });
  }

  reset(): Observable<Poll> {
    return this.http.post<Poll>(`${this.baseUrl}/reset`, {});
  }
}
