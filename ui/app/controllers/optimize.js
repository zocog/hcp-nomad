import Controller from '@ember/controller';
import { action } from '@ember/object';
import { tracked } from '@glimmer/tracking';
import { sort } from '@ember/object/computed';
import { task, timeout } from 'ember-concurrency';
import Ember from 'ember';
import { htmlSafe } from '@ember/string';

export default class OptimizeController extends Controller {
  @tracked recommendationSummaryIndex = 0;
  @tracked recommendationCardHeight = 0;

  @tracked interstitialComponent;
  @tracked error;

  summarySorting = ['submitTime:desc'];
  @sort('model', 'summarySorting') sortedSummaries;

  get activeRecommendationSummary() {
    return this.sortedSummaries.objectAt(this.recommendationSummaryIndex);
  }

  get interstitialStyle() {
    return htmlSafe(`height: ${this.recommendationCardHeight}px`);
  }

  @action
  async onProcessed() {
    this.interstitialComponent = 'accepted';
  }

  @action
  async onDismissed() {
    this.interstitialComponent = 'dismissed';
  }

  @action
  async onError(error) {
    this.interstitialComponent = 'error';
    this.error = error.toString();
  }

  @(task(function*(manual) {
    if (manual) {
      yield manual;
    } else {
      yield timeout(Ember.testing ? 0 : 2000);
    }

    this.recommendationSummaryIndex++;

    if (this.recommendationSummaryIndex >= this.model.length) {
      this.store.unloadAll('recommendation-summary');
      yield timeout(Ember.testing ? 0 : 1000);
      this.store.findAll('recommendation-summary');
    }

    this.interstitialComponent = undefined;
    this.error = undefined;
  }).drop())
  proceed;

  @action
  recommendationCardDestroying(element) {
    this.recommendationCardHeight = element.clientHeight;
  }
}
